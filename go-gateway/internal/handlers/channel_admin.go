package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/channel"
)

// ChannelAdminHandler 暴露账号渠道的登录与账号管理接口。它直接与 internal/channel
// 对话，不经过 provider 层——登录是渠道自己的事，与推理协议无关。
//
// 凭据只在本机文件里，接口一律不回传 token 明文：账号列表只给帐号标识与有效期。
type ChannelAdminHandler struct {
	registry *channel.Registry
	logger   *log.Logger

	mu       sync.Mutex
	pendings map[string]*pendingLogin
}

// pendingLogin 是一次进行中的登录：设备码请求立刻返回给用户，授权轮询在后台
// 协程里跑，前端用 id 轮询进度。
type pendingLogin struct {
	channel string
	session channel.LoginSession
	done    chan struct{}
	cred    *channel.Credential
	err     error
}

// NewChannelAdminHandler 创建渠道管理 handler。registry 为 nil 时所有端点返回 503。
func NewChannelAdminHandler(registry *channel.Registry, logger *log.Logger) *ChannelAdminHandler {
	return &ChannelAdminHandler{
		registry: registry,
		logger:   logger,
		pendings: make(map[string]*pendingLogin),
	}
}

func (h *ChannelAdminHandler) resolve(c *gin.Context) (channel.Channel, bool) {
	if h.registry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "账号渠道未启用"})
		return nil, false
	}
	name := c.Param("channel")
	ch, ok := h.registry.Get(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "未知渠道: " + name})
		return nil, false
	}
	return ch, true
}

// HandleList GET /admin/channels
func (h *ChannelAdminHandler) HandleList(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusOK, gin.H{"channels": []any{}})
		return
	}
	out := make([]gin.H, 0, h.registry.Len())
	for _, name := range h.registry.Names() {
		ch, _ := h.registry.Get(name)
		out = append(out, h.channelStatus(ch))
	}
	c.JSON(http.StatusOK, gin.H{"channels": out})
}

// HandleStatus GET /admin/channels/:channel
func (h *ChannelAdminHandler) HandleStatus(c *gin.Context) {
	ch, ok := h.resolve(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.channelStatus(ch))
}

func (h *ChannelAdminHandler) channelStatus(ch channel.Channel) gin.H {
	accounts := ch.Accounts()
	return gin.H{
		"channel":   ch.Name(),
		"logged_in": accounts.LoggedIn(),
		"accounts":  accounts.Credentials(),
	}
}

// HandleLoginStart POST /admin/channels/:channel/login
// 发起设备码登录，立刻返回验证地址与用户码；授权完成情况用 login_id 轮询。
func (h *ChannelAdminHandler) HandleLoginStart(c *gin.Context) {
	ch, ok := h.resolve(c)
	if !ok {
		return
	}
	session, err := ch.BeginLogin(c.Request.Context())
	if err != nil {
		h.logger.Printf("channel %s login start failed: %v", ch.Name(), err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	dc := session.DeviceCode()
	id := newLoginID()
	pending := &pendingLogin{channel: ch.Name(), session: session, done: make(chan struct{})}
	h.mu.Lock()
	h.pendings[id] = pending
	h.mu.Unlock()

	// 后台等待授权：超时略长于设备码有效期，保证轮询到最后一刻。
	timeout := dc.ExpiresIn
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout+30*time.Second)
		defer cancel()
		cred, err := session.Wait(ctx)
		pending.cred, pending.err = cred, err
		close(pending.done)
	}()

	c.JSON(http.StatusOK, gin.H{
		"login_id":                  id,
		"channel":                   ch.Name(),
		"user_code":                 dc.UserCode,
		"verification_uri":          dc.VerificationURI,
		"verification_uri_complete": dc.VerificationURIComplete,
		"login_url":                 dc.LoginURL(),
		"expires_in":                int(dc.ExpiresIn.Seconds()),
		"interval":                  int(dc.Interval.Seconds()),
	})
}

// HandleLoginPoll GET /admin/channels/:channel/login/:id
func (h *ChannelAdminHandler) HandleLoginPoll(c *gin.Context) {
	ch, ok := h.resolve(c)
	if !ok {
		return
	}
	id := c.Param("id")
	h.mu.Lock()
	pending, found := h.pendings[id]
	h.mu.Unlock()
	if !found || pending.channel != ch.Name() {
		c.JSON(http.StatusNotFound, gin.H{"error": "登录会话不存在或已结束"})
		return
	}

	select {
	case <-pending.done:
		// 终态：清理会话，返回结果。
		h.mu.Lock()
		delete(h.pendings, id)
		h.mu.Unlock()
		if pending.err != nil {
			status := "failed"
			if errors.Is(pending.err, channel.ErrLoginCancelled) {
				status = "cancelled"
			}
			c.JSON(http.StatusOK, gin.H{"status": status, "error": pending.err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":   "complete",
			"channel":  ch.Name(),
			"account":  accountNickname(pending.cred),
			"accounts": ch.Accounts().Credentials(),
		})
	default:
		c.JSON(http.StatusAccepted, gin.H{"status": "pending"})
	}
}

// HandleLogout DELETE /admin/channels/:channel
func (h *ChannelAdminHandler) HandleLogout(c *gin.Context) {
	ch, ok := h.resolve(c)
	if !ok {
		return
	}
	if err := ch.Accounts().RemoveAll(); err != nil {
		h.logger.Printf("channel %s logout failed: %v", ch.Name(), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"channel": ch.Name(), "logged_in": false, "accounts": ch.Accounts().Credentials()})
}

// HandleRemoveAccount DELETE /admin/channels/:channel/accounts/:index
func (h *ChannelAdminHandler) HandleRemoveAccount(c *gin.Context) {
	ch, ok := h.resolve(c)
	if !ok {
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "账号序号非法"})
		return
	}
	if err := ch.Accounts().RemoveAccountAt(index); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"channel": ch.Name(), "accounts": ch.Accounts().Credentials()})
}

// accountNickname 返回新登录账号的展示名，供前端即时反馈。
func accountNickname(cred *channel.Credential) string {
	if cred == nil {
		return ""
	}
	if cred.Nickname != "" {
		return cred.Nickname
	}
	return cred.AccountID
}

func newLoginID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

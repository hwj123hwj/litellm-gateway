package provider

// ChatGPT Codex Provider：使用 ChatGPT OAuth token 调用 chatgpt.com 的 Codex API。
// 不需要第三方中转，直接用你自己的 ChatGPT Plus/Pro 订阅。
// 请求和响应都是 OpenAI Responses API 格式，直接透传，不需要格式转换。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	chatgptCodexURL = "https://chatgpt.com/backend-api/codex/responses"
	chatgptTokenURL = "https://auth.openai.com/oauth/token"
	chatgptClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// chatgptAuth 存储 ChatGPT OAuth 凭证
type chatgptAuth struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
}

// chatgptAuthFile 是 ~/.codex/auth.json 的格式
type chatgptAuthFile struct {
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

// ChatGPTProvider 使用 ChatGPT OAuth token 调用 Codex API
type ChatGPTProvider struct {
	mu          sync.RWMutex
	auth        *chatgptAuth
	authPath    string
	proxyURL    string // http://127.0.0.1:7890
	client      *http.Client
	lastRefresh time.Time
}

// NewChatGPTProvider 创建 ChatGPT provider
func NewChatGPTProvider(proxyURL string) *ChatGPTProvider {
	p := &ChatGPTProvider{
		authPath: filepath.Join(os.Getenv("HOME"), ".codex", "auth.json"),
		proxyURL: proxyURL,
	}

	// 构建 HTTP client（带可选代理）
	transport := &http.Transport{}
	if proxyURL != "" {
		if proxy, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
		}
	}
	p.client = &http.Client{
		Transport: transport,
		Timeout:   300 * time.Second, // Codex 请求可能比较长
	}

	// 初始加载 token
	_ = p.loadAuth()

	return p
}

func (p *ChatGPTProvider) Name() string    { return "chatgpt" }
func (p *ChatGPTProvider) URL() string     { return chatgptCodexURL }
func (p *ChatGPTProvider) APIKey() string  { return p.getAccessToken() }
func (p *ChatGPTProvider) UseBearer() bool { return true }

func (p *ChatGPTProvider) getAccessToken() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.auth != nil {
		return p.auth.AccessToken
	}
	return ""
}

func (p *ChatGPTProvider) getAccountID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.auth != nil {
		return p.auth.AccountID
	}
	return ""
}

// loadAuth 从 ~/.codex/auth.json 读取 OAuth token
func (p *ChatGPTProvider) loadAuth() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := os.ReadFile(p.authPath)
	if err != nil {
		return fmt.Errorf("read auth.json: %w", err)
	}

	var file chatgptAuthFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse auth.json: %w", err)
	}

	if file.Tokens.AccessToken == "" {
		return fmt.Errorf("auth.json has no access_token")
	}

	p.auth = &chatgptAuth{
		AccessToken:  file.Tokens.AccessToken,
		RefreshToken: file.Tokens.RefreshToken,
		AccountID:    file.Tokens.AccountID,
	}
	return nil
}

// refreshAuth 使用 refresh_token 刷新 access_token
func (p *ChatGPTProvider) refreshAuth() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.auth == nil || p.auth.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	// 防止频繁刷新
	if time.Since(p.lastRefresh) < 30*time.Second {
		return nil
	}
	p.lastRefresh = time.Now()

	body := strings.NewReader(strings.Join([]string{
		"grant_type=refresh_token",
		"&refresh_token=" + url.QueryEscape(p.auth.RefreshToken),
		"&client_id=" + chatgptClientID,
	}, ""))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, chatgptTokenURL, body)
	if err != nil {
		return fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send refresh request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return NewHTTPError("chatgpt-auth", resp, respBody)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("parse refresh response: %w", err)
	}

	if result.AccessToken == "" {
		return fmt.Errorf("refresh response missing access_token")
	}

	p.auth.AccessToken = result.AccessToken
	if result.RefreshToken != "" {
		p.auth.RefreshToken = result.RefreshToken
	}

	// 提取 account_id from JWT
	parts := strings.Split(result.AccessToken, ".")
	if len(parts) == 3 {
		p.auth.AccountID = extractAccountIDFromJWT(parts[1])
	}

	return nil
}

// extractAccountIDFromJWT 从 JWT payload 中提取 chatgpt_account_id
func extractAccountIDFromJWT(payload string) string {
	// Base64url decode
	s := payload
	// Add padding
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}

	// 手动 base64url 解码
	decoded := make([]byte, len(s)*3/4)
	var idx int
	for i := 0; i+4 <= len(s); i += 4 {
		var val uint32
		for j := 0; j < 4; j++ {
			c := s[i+j]
			switch {
			case c >= 'A' && c <= 'Z':
				val = val*64 + uint32(c-'A')
			case c >= 'a' && c <= 'z':
				val = val*64 + uint32(c-'a'+26)
			case c >= '0' && c <= '9':
				val = val*64 + uint32(c-'0'+52)
			case c == '-':
				val = val*64 + 62
			case c == '_':
				val = val*64 + 63
			default:
				continue
			}
		}
		decoded[idx] = byte(val >> 16)
		decoded[idx+1] = byte(val >> 8)
		decoded[idx+2] = byte(val)
		idx += 3
	}
	decoded = decoded[:idx]

	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(decoded, &claims); err == nil {
		return claims.Auth.AccountID
	}
	return ""
}

// ForwardRequest 实现 Provider 接口（非流式）
// 注意：ChatGPT Codex API 不支持非流式，所以这里始终用流式然后收集结果
func (p *ChatGPTProvider) ForwardRequest(ctx context.Context, req *Request) (*Response, error) {
	// ChatGPT API 要求 stream=true，所以用流式然后收集
	// 但这里的 req 是 Anthropic 格式...我们需要转成 Responses API 格式
	// 实际上在 responses handler 中，chatgpt provider 走的是透传路径，不会到这里
	return nil, fmt.Errorf("ChatGPT provider only supports streaming via Responses API passthrough")
}

// IsHealthy 检查 ChatGPT provider 是否可用
func (p *ChatGPTProvider) IsHealthy(ctx context.Context) bool {
	return p.getAccessToken() != ""
}

// ForwardRawResponses 实现原始 Responses API 透传
// 请求和响应都是 Responses API 格式，不需要格式转换
// 返回的 SSE 流直接写入 w
func (p *ChatGPTProvider) ForwardRawResponsesStream(ctx context.Context, reqBody json.RawMessage, w io.Writer) error {
	accessToken := p.getAccessToken()
	accountID := p.getAccountID()
	if accessToken == "" {
		// 尝试重新加载
		if err := p.loadAuth(); err != nil {
			return fmt.Errorf("no ChatGPT access token: %w", err)
		}
		accessToken = p.getAccessToken()
		accountID = p.getAccountID()
	}

	// 注入必要字段：store=false, stream=true
	var reqMap map[string]json.RawMessage
	if err := json.Unmarshal(reqBody, &reqMap); err != nil {
		return fmt.Errorf("parse request: %w", err)
	}
	reqMap["store"] = json.RawMessage(`false`)
	reqMap["stream"] = json.RawMessage(`true`)
	// 如果没有 instructions，加一个默认的
	if _, ok := reqMap["instructions"]; !ok {
		reqMap["instructions"] = json.RawMessage(`"You are a helpful coding assistant."`)
	}

	finalBody, err := json.Marshal(reqMap)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, bytes.NewReader(finalBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("chatgpt-account-id", accountID)
	httpReq.Header.Set("OpenAI-Beta", "responses=experimental")
	httpReq.Header.Set("originator", "go-gateway")
	httpReq.Header.Set("User-Agent", "go-llm-gateway/1.0")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		// 可能是 token 过期，尝试刷新
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			if refreshErr := p.refreshAuth(); refreshErr == nil {
				// 重试一次
				httpReq.Header.Set("Authorization", "Bearer "+p.getAccessToken())
				resp, err = p.client.Do(httpReq)
			}
		}
		if err != nil {
			return fmt.Errorf("send request to ChatGPT: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// 如果是 401，尝试刷新 token 后重试
		if resp.StatusCode == 401 {
			if refreshErr := p.refreshAuth(); refreshErr == nil {
				// 构建新请求重试
				retryReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, bytes.NewReader(finalBody))
				retryReq.Header = httpReq.Header.Clone()
				retryReq.Header.Set("Authorization", "Bearer "+p.getAccessToken())

				retryResp, retryErr := p.client.Do(retryReq)
				if retryErr != nil {
					return fmt.Errorf("retry after refresh: %w", retryErr)
				}
				defer retryResp.Body.Close()

				if retryResp.StatusCode != http.StatusOK {
					retryBody, _ := io.ReadAll(retryResp.Body)
					return NewHTTPError(p.Name(), retryResp, retryBody)
				}

				// 透传 SSE 流
				_, err = io.Copy(w, retryResp.Body)
				return err
			}
		}
		return NewHTTPError(p.Name(), resp, body)
	}

	// 透传 SSE 流（ChatGPT 返回的就是标准 Responses API SSE）
	_, err = io.Copy(w, resp.Body)
	return err
}

// ChatGPTPassthroughMarker 标记此 provider 支持 Responses API 原始透传
type ChatGPTPassthroughMarker interface {
	ForwardRawResponsesStream(ctx context.Context, reqBody json.RawMessage, w io.Writer) error
}

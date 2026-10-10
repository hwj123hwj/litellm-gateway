package channel

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// refreshLead 是「提前多久续期」的阈值：距过期不足这个时长才刷新，避免每次请求
// 都去轮换 refresh_token（上游会因此失效）。
const refreshLead = time.Hour

// refreshFn 对一个凭据执行一次续期并返回新凭据（不触碰存储）。
type refreshFn func(ctx context.Context, cred Credential) (Credential, error)

// Accounts 管理一个渠道的账号池：内存缓存 + 落盘存储 + 令牌续期。
//
// 它同时是 provider 层 AuthSource 与 CredentialReporter 的结构化实现：
// provider 只做接口断言，channel 包不 import provider，避免循环依赖。
type Accounts struct {
	channel string
	store   *Store
	client  *oauthClient
	refresh refreshFn

	mu    sync.RWMutex
	creds []Credential
	index int
}

// NewAccounts 构造账号池并立即从存储载入已有凭据。
func NewAccounts(channel string, store *Store, client *oauthClient, refresh refreshFn) *Accounts {
	a := &Accounts{channel: channel, store: store, client: client, refresh: refresh}
	a.reload()
	return a
}

func (a *Accounts) reload() {
	creds, err := a.store.Load(a.channel)
	if err != nil {
		creds = nil
	}
	a.mu.Lock()
	a.creds = creds
	if a.index >= len(creds) {
		a.index = 0
	}
	a.mu.Unlock()
}

// Store 暴露凭据存储，供登录流程写入。
func (a *Accounts) Store() *Store { return a.store }

// Channel 实现 CredentialReporter。
func (a *Accounts) Channel() string { return a.channel }

// LoggedIn 实现 CredentialReporter：至少有一个可用账号即为已登录。
func (a *Accounts) LoggedIn() bool { return a.BearerToken() != "" }

// BearerToken 实现 AuthSource：返回当前账号的访问令牌。空串表示未登录。
func (a *Accounts) BearerToken() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	cred := a.currentLocked()
	if cred == nil {
		return ""
	}
	return cred.AccessToken
}

// Credentials 实现 CredentialReporter：返回所有账号的非敏感摘要。
// 注意只回传账号标识与有效期，绝不回传 token 明文。
func (a *Accounts) Credentials() []credentialSummary {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := time.Now()
	out := make([]credentialSummary, 0, len(a.creds))
	for i := range a.creds {
		cred := &a.creds[i]
		summary := credentialSummary{
			AccountID: cred.AccountID,
			Nickname:  cred.Nickname,
			Expired:   cred.Expired(now),
		}
		if !cred.ExpiresAt.IsZero() {
			summary.ExpiresAt = cred.ExpiresAt.UTC().Format(time.RFC3339)
		}
		out = append(out, summary)
	}
	return out
}

// credentialSummary 是 channel 层的非敏感凭据摘要（provider.CredentialSummary 的
// 结构化对应物，字段名一致以便 Admin API 直接序列化）。
type credentialSummary struct {
	AccountID string `json:"account_id,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Expired   bool   `json:"expired"`
}

// Refresh 实现 AuthSource：当前账号临近过期或已过期时续期一次。
// 返回 true 表示令牌已更新。续期失败但仍有其他账号时会自动换号。
func (a *Accounts) Refresh(ctx context.Context) bool {
	a.mu.Lock()
	cred := a.currentLocked()
	a.mu.Unlock()
	if cred == nil {
		return false
	}
	if !a.refreshable(cred) {
		a.advance()
		return a.BearerToken() != ""
	}

	next, err := a.refresh(ctx, *cred)
	if err != nil {
		// 续期被上游拒绝（refresh_token 失效）时换下一个账号，而不是死磕一个。
		a.advance()
		return a.BearerToken() != ""
	}
	a.replace(*cred, next)
	return true
}

// ensureFresh 在发送请求前按 lead-time 静默续期，避免第一个请求先撞一次 401。
func (a *Accounts) ensureFresh(ctx context.Context) {
	a.mu.RLock()
	cred := a.currentLocked()
	a.mu.RUnlock()
	if cred == nil || !a.needsRefresh(cred) {
		return
	}
	if next, err := a.refresh(ctx, *cred); err == nil {
		a.replace(*cred, next)
	}
}

func (a *Accounts) refreshable(cred *Credential) bool {
	return cred.RefreshToken != ""
}

func (a *Accounts) needsRefresh(cred *Credential) bool {
	return cred.Expired(time.Now().Add(refreshLead))
}

// currentLocked 返回当前选中的凭据；调用方须持有锁。
func (a *Accounts) currentLocked() *Credential {
	if len(a.creds) == 0 {
		return nil
	}
	if a.index < 0 || a.index >= len(a.creds) {
		a.index = 0
	}
	return &a.creds[a.index]
}

// advance 切到下一个账号（多账号轮换）。
func (a *Accounts) advance() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.creds) > 1 {
		a.index = (a.index + 1) % len(a.creds)
	}
}

// replace 用续期后的凭据覆盖原凭据并落盘。
func (a *Accounts) replace(old, next Credential) {
	a.mu.Lock()
	index := -1
	for i := range a.creds {
		if a.sameAccount(&a.creds[i], &old) {
			index = i
			break
		}
	}
	if index >= 0 {
		a.creds[index] = next
	}
	a.mu.Unlock()
	if index >= 0 {
		_ = a.store.ReplaceAccount(a.channel, index, next)
	}
}

// add 新增一条账号凭据并落盘，随后重新载入内存。
func (a *Accounts) add(cred Credential) error {
	if err := a.store.UpsertAccount(a.channel, cred); err != nil {
		return fmt.Errorf("save %s credential: %w", a.channel, err)
	}
	a.reload()
	return nil
}

func (a *Accounts) sameAccount(x, y *Credential) bool {
	if x.AccountID != "" && y.AccountID != "" {
		return x.AccountID == y.AccountID
	}
	return x.AccessToken == y.AccessToken
}

// Len 返回账号数量（含已过期）。
func (a *Accounts) Len() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.creds)
}

// RemoveAccountAt 删除第 index 个账号并落盘，随后重载内存。
func (a *Accounts) RemoveAccountAt(index int) error {
	if err := a.store.RemoveAccount(a.channel, index); err != nil {
		return err
	}
	a.reload()
	return nil
}

// RemoveAll 清空本渠道的全部凭据（登出）。
func (a *Accounts) RemoveAll() error {
	if err := a.store.Clear(a.channel); err != nil {
		return err
	}
	a.mu.Lock()
	a.creds = nil
	a.index = 0
	a.mu.Unlock()
	return nil
}

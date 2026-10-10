// Package channel 实现「账号渠道」：用交互式登录（设备码 / 浏览器回调）拿到的
// 第三方产品凭据，代替 API Key 调用其私有推理端点。
//
// 与本仓库既有 provider 的差别：这些上游不是公开 API，鉴权靠复刻官方客户端的
// 登录与请求头。凭据需要交互式获取、会过期、要续期，因此单独一层，而不是塞进
// providers.yaml 的静态 api_key_env。
//
// 设计边界：
//   - 凭据只落本机文件（0700 目录 / 0600 文件），不进 providers.yaml、不进日志、
//     不进 Admin API 响应；
//   - 每个渠道一个 Provider 实现，共享这里的 Credential 存储与刷新调度；
//   - 登录是显式的用户动作，由 Admin API 触发，不随进程启动自动发起。
package channel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Credential 是某个渠道的一条账号凭据。字段按各渠道实际需要存取：所有渠道都用
// AccessToken/RefreshToken/ExpiresAt，其余（AccountID、MachineID、UID 等）按渠道
// 自取。Extra 用于渠道特有的字段，避免为每个渠道加一列。
type Credential struct {
	AccessToken  string            `json:"access_token"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time         `json:"expires_at,omitempty"`
	AccountID    string            `json:"account_id,omitempty"`
	Nickname     string            `json:"nickname,omitempty"`
	Extra        map[string]string `json:"extra,omitempty"`
}

// Expired 报告凭据是否已过期。ExpiresAt 为零值表示上游未给出过期时间，
// 此时不判过期（由调用方在 401 时触发刷新）。
func (c *Credential) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !c.ExpiresAt.After(now)
}

// Fetcher 是登录流程产出的凭据集合：一个渠道可以有多个账号，按优先级轮换。
type Fetcher interface {
	// Channel 返回渠道标识（与 providers.yaml 的 provider name 一致）。
	Channel() string
	// Load 返回当前所有账号凭据（不含明文诊断）。
	Load() ([]Credential, error)
}

// Store 按渠道持久化账号凭据。一个渠道一个文件，内容为凭据数组。
// 文件权限 0600，目录 0700；写入用临时文件 + rename，避免半截文件。
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore 在 dir 下创建凭据存储。dir 通常为 <gatewayHome>/channels。
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) path(channel string) string {
	return filepath.Join(s.dir, channel+".json")
}

// Load 读取渠道的全部账号凭据。文件不存在时返回空切片而非错误——未登录是
// 正常状态，不应阻断其他渠道。
func (s *Store) Load(channel string) ([]Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path(channel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s credentials: %w", channel, err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var creds []Credential
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse %s credentials: %w", channel, err)
	}
	return creds, nil
}

// Save 覆盖写入渠道的全部账号凭据。
func (s *Store) Save(channel string, creds []Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(channel, creds)
}

func (s *Store) saveLocked(channel string, creds []Credential) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create channel dir: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s credentials: %w", channel, err)
	}
	data = append(data, '\n')

	final := s.path(channel)
	tmp, err := os.CreateTemp(s.dir, channel+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp credential file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后此处为 no-op

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp credential file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp credential file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp credential file: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		return fmt.Errorf("replace %s credentials: %w", channel, err)
	}
	return nil
}

// Has 报告渠道是否已有可用账号。
func (s *Store) Has(channel string) bool {
	creds, err := s.Load(channel)
	return err == nil && len(creds) > 0
}

// UpsertAccount 新增或按 AccountID 替换一条账号凭据并落盘。
// AccountID 为空时按 AccessToken 判重。
func (s *Store) UpsertAccount(channel string, cred Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.loadLocked(channel)
	if err != nil {
		return err
	}
	key := cred.AccountID
	if key == "" {
		key = cred.AccessToken
	}
	replaced := false
	for i := range creds {
		existingKey := creds[i].AccountID
		if existingKey == "" {
			existingKey = creds[i].AccessToken
		}
		if existingKey == key {
			creds[i] = cred
			replaced = true
			break
		}
	}
	if !replaced {
		creds = append(creds, cred)
	}
	return s.saveLocked(channel, creds)
}

// ReplaceAccount 就地更新第一条账号凭据（用于续期后写回 token）。
func (s *Store) ReplaceAccount(channel string, index int, cred Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.loadLocked(channel)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(creds) {
		return fmt.Errorf("credential index %d out of range for %s", index, channel)
	}
	creds[index] = cred
	return s.saveLocked(channel, creds)
}

// Clear 删除整个渠道的凭据文件（登出）。文件不存在时视为成功。
func (s *Store) Clear(channel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(channel)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear %s credentials: %w", channel, err)
	}
	return nil
}

// RemoveAccount 删除指定序号的账号凭据。
func (s *Store) RemoveAccount(channel string, index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.loadLocked(channel)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(creds) {
		return fmt.Errorf("credential index %d out of range for %s", index, channel)
	}
	creds = append(creds[:index], creds[index+1:]...)
	if len(creds) == 0 {
		// 最后一个账号被删除时清掉文件，让「未登录」回到干净状态。
		if err := os.Remove(s.path(channel)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s credentials: %w", channel, err)
		}
		return nil
	}
	return s.saveLocked(channel, creds)
}

func (s *Store) loadLocked(channel string) ([]Credential, error) {
	data, err := os.ReadFile(s.path(channel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s credentials: %w", channel, err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var creds []Credential
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse %s credentials: %w", channel, err)
	}
	return creds, nil
}

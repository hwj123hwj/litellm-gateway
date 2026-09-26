// Package memory implements the agent long-term memory data plane: scoped,
// propositional memory entries with an explicit governance lifecycle
// (agent drafts → human confirms → active; stale → retired).
//
// 设计原则（与 PRD R7 分工一致）：
//   - 网关只负责存储、作用域路由、命中观测与导出，不做知识编译；
//   - 记忆条目是可检验的命题（带来源与时间），不是聊天摘要；
//   - agent 写入只能进 candidate（起草），active 必须经 admin 确认；
//   - 密钥类内容在写入前被拒绝（与"Token/密钥永不落库"红线一致）。
package memory

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Config controls the memory data plane. Disabled by default; the memory
// handler then behaves as an empty store so the dashboard needs no flag.
type Config struct {
	Enabled bool
}

// DefaultConfig returns the safe default (disabled).
func DefaultConfig() Config {
	return Config{Enabled: false}
}

// Status is the governance lifecycle of a memory entry.
type Status string

const (
	// StatusCandidate means agent-drafted, waiting for human confirmation.
	StatusCandidate Status = "candidate"
	// StatusActive means confirmed and eligible for retrieval/injection.
	StatusActive Status = "active"
	// StatusRetired means stale or contradicted; kept for audit, never injected.
	StatusRetired Status = "retired"
)

// ScopeType bounds where a memory applies. 跨作用域污染是记忆系统的头号
// 故障模式，因此每条记忆必须带作用域，检索时按 user 声明的作用域路由。
type ScopeType string

const (
	ScopeGlobal ScopeType = "global" // 跨项目通用（极少）：语言偏好等
	ScopeClient ScopeType = "client" // 按客户端（HwjCode / Claude Code / Codex CLI / Pi）
	ScopeProject ScopeType = "project" // 按仓库：scope_key = host:owner/repo
)

// StatementMaxLen caps a single memory statement. 记忆是命题不是摘要。
const StatementMaxLen = 500

// Memory is one propositional memory entry.
type Memory struct {
	ID         int64     `json:"id"`
	ScopeType  ScopeType `json:"scope_type"`
	ScopeKey   string    `json:"scope_key"`
	Statement  string    `json:"statement"`
	Status     Status    `json:"status"`
	Source     string    `json:"source"` // manual | agent:<client> | archive:<request_id> | git:<sha>
	Confidence float64   `json:"confidence"`
	HitCount   int       `json:"hit_count"`
	LastHitAt  *time.Time `json:"last_hit_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ListFilter narrows admin listings.
type ListFilter struct {
	Status    Status
	ScopeType ScopeType
	ScopeKey  string
	Limit     int
	Offset    int
}

// Store persists memory entries. A NoopStore is wired when the feature is
// disabled; endpoints keep responding with empty results so callers and the
// dashboard need no feature flag.
type Store interface {
	// Insert stores a new entry; duplicates (same scope + statement) are
	// absorbed by refreshing the existing row instead of inserting a copy.
	Insert(m Memory) (Memory, bool, error)
	Get(id int64) (*Memory, error)
	List(filter ListFilter) ([]Memory, int, error)
	SetStatus(id int64, status Status) (bool, error)
	Delete(id int64) (bool, error)
	// LookupFor merges global + client + project active memories, ordered by
	// confidence then recency, capped at limit. 依据作用域路由检索。
	LookupFor(client, project string, limit int) ([]Memory, error)
	// MarkHits records retrieval hits — the feedback signal that later drives
	// 遗忘（长期零命中的条目应被降权/清理）。
	MarkHits(ids []int64) error
}

// NoopStore implements Store as a no-op. Used when the feature is disabled.
type NoopStore struct{}

func (NoopStore) Insert(m Memory) (Memory, bool, error)         { return Memory{}, false, nil }
func (NoopStore) Get(int64) (*Memory, error)                    { return nil, nil }
func (NoopStore) List(ListFilter) ([]Memory, int, error)        { return nil, 0, nil }
func (NoopStore) SetStatus(int64, Status) (bool, error)         { return false, nil }
func (NoopStore) Delete(int64) (bool, error)                    { return false, nil }
func (NoopStore) LookupFor(string, string, int) ([]Memory, error) { return nil, nil }
func (NoopStore) MarkHits([]int64) error                        { return nil }

// secretPatterns are rejected outright at write time — Authorization、Token、
// 密码和 API Key 永不落库（AGENTS.md 红线）。这里是最后一道守门。
var secretPatterns = regexp.MustCompile(
	`(?i)(sk-[a-z0-9]{8,}|ghp_[a-z0-9]{20,}|gho_[a-z0-9]{20,}|xox[baprs]-[a-z0-9-]{10,}|akia[a-z0-9]{12,}|-----begin [a-z ]*private key-----|password\s*[:=])`)

// ErrSecretLike is returned when a statement looks like it carries a secret.
var ErrSecretLike = errors.New("statement 疑似包含密钥/凭据，拒绝入库")

// SanitizeStatement validates and normalizes a statement: trims, caps length,
// and rejects secret-like content. Everything else (HTML, punctuation) is the
// author's business.
func SanitizeStatement(raw string) (string, error) {
	statement := strings.TrimSpace(raw)
	if statement == "" {
		return "", fmt.Errorf("statement 不能为空")
	}
	if len(statement) > StatementMaxLen {
		return "", fmt.Errorf("statement 超长（最多 %d 字符）", StatementMaxLen)
	}
	if secretPatterns.MatchString(statement) {
		return "", ErrSecretLike
	}
	return statement, nil
}

// NormalizeScope validates scope_type/scope_key pairs. project 作用域必须带
// host:owner/repo 形态的 key，global 作用域不允许 key——避免"看起来有作用域
// 实际全局生效"的条目混进来。
func NormalizeScope(scopeType ScopeType, scopeKey string) (ScopeType, string, error) {
	key := strings.TrimSpace(scopeKey)
	switch ScopeType(scopeType) {
	case ScopeGlobal:
		if key != "" {
			return "", "", fmt.Errorf("global 作用域不允许 scope_key")
		}
		return ScopeGlobal, "", nil
	case ScopeClient:
		if key == "" {
			return "", "", fmt.Errorf("client 作用域必须提供 scope_key（客户端名）")
		}
		return ScopeClient, key, nil
	case ScopeProject:
		if key == "" {
			return "", "", fmt.Errorf("project 作用域必须提供 scope_key（host:owner/repo）")
		}
		return ScopeProject, key, nil
	default:
		return "", "", fmt.Errorf("不支持的 scope_type %q（可选 global | client | project）", scopeType)
	}
}

// RenderBlock formats memories as a compact markdown block for wrapper-based
// injection (通道 B)。空结果返回空串——不注入空块浪费 token。
func RenderBlock(memories []Memory) string {
	if len(memories) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<agent-memory>\n")
	b.WriteString("以下是关于当前工作上下文的长期记忆，若与当前指令冲突请向用户确认：\n")
	for _, m := range memories {
		b.WriteString("- ")
		b.WriteString(m.Statement)
		b.WriteString("\n")
	}
	b.WriteString("</agent-memory>")
	return b.String()
}

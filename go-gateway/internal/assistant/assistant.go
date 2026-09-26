// Package assistant embeds a pi-go-based resident agent inside the gateway
// (docs/MEMORY_DESIGN.md §常驻助理). The agent's LLM calls loop back through
// the gateway's own OpenAI-compatible endpoint — dogfooding the router, so
// assistant traffic lands in request_logs and inherits provider failover.
//
// 治理边界（与记忆层一致）：助理可以**提案**记忆（candidate），但不能
// 确认/退役——人拍板只发生在 /admin/memories 治理端点上。
package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	piagent "github.com/hwj123hwj/pi-go/sdk/agent"
	"github.com/hwj123hwj/pi-go/sdk/ai"
	"github.com/hwj123hwj/pi-go/sdk/ai/providers"

	"github.com/weijian/go-llm-gateway/internal/memory"
)

// Config controls the resident assistant.
type Config struct {
	Enabled bool
	// Model 是网关 /v1/models 里的模型 ID（如 glm-5.3-flash）。
	Model string
	// BaseURL 指向网关自身（默认 http://127.0.0.1:<port>）。
	BaseURL string
	// APIKey 默认复用 MasterKey。
	APIKey string
}

// Assistant is a long-lived pi-go agent wired to gateway-domain tools.
type Assistant struct {
	mu       sync.Mutex
	agent    *piagent.Agent
	memories memory.Store
	log      *log.Logger
}

// New builds the resident agent. The LLM transport points back at the
// gateway itself, so assistant traffic is visible in the same metrics and
// failover chains as every other client.
func New(cfg Config, memories memory.Store, logger *log.Logger) (*Assistant, error) {
	if logger == nil {
		logger = log.New(nil, "", 0)
	}
	registry := providers.NewRegistry()
	registry.Register(providers.NewOpenAIProvider(cfg.APIKey, cfg.BaseURL))
	model := ai.Model{
		ID:            cfg.Model,
		Name:          cfg.Model,
		API:           "openai-completions",
		Provider:      "openai", // 必须与 provider.Name() 一致，Agent 以此解析
		BaseURL:       cfg.BaseURL,
		ContextWindow: 128000,
		MaxTokens:     8192,
	}
	agent := piagent.New(piagent.Options{
		Model:    model,
		Registry: registry,
		System:   systemPrompt,
		Tools: []piagent.Tool{
			newMemoryLookupTool(memories),
			newMemoryListPendingTool(memories),
			newMemoryProposeTool(memories),
		},
		MaxTurns: 8,
	})
	return &Assistant{agent: agent, memories: memories, log: logger}, nil
}

// StreamUpdate is one SSE-mappable update from a Chat turn.
type StreamUpdate struct {
	Type    string // text_delta | tool_start | tool_end | done | error
	Content string
	Tool    string
}

// Chat runs one user turn through the agent, forwarding streamed updates.
// 序列化执行：常驻 agent 的历史在实例内，个人场景单并发足够。
func (a *Assistant) Chat(ctx context.Context, message string, onUpdate func(StreamUpdate)) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	events, err := a.agent.PromptStream(ctx, ai.NewTextUserMessage(message))
	if err != nil {
		return "", fmt.Errorf("assistant prompt: %w", err)
	}
	var final string
	for ev := range events {
		switch ev.Type {
		case piagent.StreamEventTextDelta:
			if ev.TextDelta != "" && onUpdate != nil {
				onUpdate(StreamUpdate{Type: "text_delta", Content: ev.TextDelta})
			}
		case piagent.StreamEventToolStart:
			if onUpdate != nil {
				onUpdate(StreamUpdate{Type: "tool_start", Tool: ev.ToolName})
			}
		case piagent.StreamEventToolEnd:
			if onUpdate != nil {
				onUpdate(StreamUpdate{Type: "tool_end", Tool: ev.ToolName})
			}
		case piagent.StreamEventDone:
			if ev.FinalMessage.Text != "" {
				final = ev.FinalMessage.Text
			}
		case piagent.StreamEventError:
			return final, fmt.Errorf("assistant turn error: %s", ev.Error)
		}
	}
	if final == "" {
		return "", fmt.Errorf("assistant turn finished without output")
	}
	return final, nil
}

const systemPrompt = `你是常驻在 LLM Gateway 里的助理（网关地址即你自己的 LLM 出口）。

职责：
1. 记忆管家：按用户要求检索、审阅长期记忆；发现值得长期记住的事实时，用
   memory_propose 提案（进入 candidate，等用户在管理端确认，你无权直接生效）。
2. 网关顾问：回答关于网关自身能力、配置约定的问题。
3. 日常问答。

守则：
- 记忆提案必须是命题化的陈述（一句话可检验），不要存聊天摘要或一次性细节；
- 任何密钥、token、密码类内容一律不写入记忆，也不要复述；
- 检索记忆时优先按用户提到的项目作用域（project scope_key 形如 host:owner/repo）；
- 回答用中文，简洁直接。`

// ── 网关域工具 ────────────────────────────────────────────────────────────

type memoryLookupTool struct{ store memory.Store }

func newMemoryLookupTool(store memory.Store) piagent.Tool { return &memoryLookupTool{store: store} }

func (t *memoryLookupTool) Name() string { return "memory_lookup" }
func (t *memoryLookupTool) Description() string {
	return "按作用域检索已生效（active）的长期记忆。project 传 host:owner/repo 形态的仓库作用域；client 传客户端名。"
}
func (t *memoryLookupTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"project": map[string]any{"type": "string", "description": "仓库作用域，如 github:hwj123hwj/litellm-gateway"},
			"client":  map[string]any{"type": "string", "description": "客户端作用域，如 claude-code"},
		},
	}
}
func (t *memoryLookupTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	return params, nil
}
func (t *memoryLookupTool) Execute(ctx context.Context, params json.RawMessage, onUpdate func(piagent.PartialResult)) (piagent.ToolResult, error) {
	var args struct {
		Project string `json:"project"`
		Client  string `json:"client"`
	}
	_ = json.Unmarshal(params, &args)
	memories, err := t.store.LookupFor(strings.TrimSpace(args.Client), strings.TrimSpace(args.Project), 20)
	if err != nil {
		return piagent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if len(memories) == 0 {
		return piagent.ToolResult{Content: "（该作用域暂无生效记忆）"}, nil
	}
	var b strings.Builder
	for _, m := range memories {
		fmt.Fprintf(&b, "- [%s] %s\n", m.ScopeType, m.Statement)
	}
	return piagent.ToolResult{Content: b.String()}, nil
}

type memoryListPendingTool struct{ store memory.Store }

func newMemoryListPendingTool(store memory.Store) piagent.Tool {
	return &memoryListPendingTool{store: store}
}

func (t *memoryListPendingTool) Name() string { return "memory_list_pending" }
func (t *memoryListPendingTool) Description() string {
	return "列出待确认的候选记忆（candidate），可据此向用户建议确认或删除。"
}
func (t *memoryListPendingTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *memoryListPendingTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	return params, nil
}
func (t *memoryListPendingTool) Execute(ctx context.Context, params json.RawMessage, onUpdate func(piagent.PartialResult)) (piagent.ToolResult, error) {
	memories, _, err := t.store.List(memory.ListFilter{Status: memory.StatusCandidate, Limit: 20})
	if err != nil {
		return piagent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if len(memories) == 0 {
		return piagent.ToolResult{Content: "（暂无待确认候选）"}, nil
	}
	var b strings.Builder
	for _, m := range memories {
		fmt.Fprintf(&b, "#%d [%s %s] %s\n", m.ID, m.ScopeType, m.ScopeKey, m.Statement)
	}
	return piagent.ToolResult{Content: b.String()}, nil
}

type memoryProposeTool struct{ store memory.Store }

func newMemoryProposeTool(store memory.Store) piagent.Tool { return &memoryProposeTool{store: store} }

func (t *memoryProposeTool) Name() string { return "memory_propose" }
func (t *memoryProposeTool) Description() string {
	return "提案一条长期记忆（进入 candidate，等用户在管理端确认后生效）。必须是命题化陈述；密钥/凭据会被拒绝。"
}
func (t *memoryProposeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"statement":  map[string]any{"type": "string", "description": "命题化陈述，一句话可检验"},
			"scope_type": map[string]any{"type": "string", "description": "global | client | project", "enum": []string{"global", "client", "project"}},
			"scope_key":  map[string]any{"type": "string", "description": "project: host:owner/repo；client: 客户端名；global 留空"},
		},
		"required": []string{"statement", "scope_type"},
	}
}
func (t *memoryProposeTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	return params, nil
}
func (t *memoryProposeTool) Execute(ctx context.Context, params json.RawMessage, onUpdate func(piagent.PartialResult)) (piagent.ToolResult, error) {
	var args struct {
		Statement string `json:"statement"`
		ScopeType string `json:"scope_type"`
		ScopeKey  string `json:"scope_key"`
	}
	_ = json.Unmarshal(params, &args)
	scopeType, scopeKey, err := memory.NormalizeScope(memory.ScopeType(args.ScopeType), args.ScopeKey)
	if err != nil {
		return piagent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	statement, err := memory.SanitizeStatement(args.Statement)
	if err != nil {
		return piagent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	entry, _, err := t.store.Insert(memory.Memory{
		ScopeType: scopeType, ScopeKey: scopeKey, Statement: statement,
		Status: memory.StatusCandidate, Source: "agent:assistant", Confidence: 0.3,
	})
	if err != nil {
		return piagent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	return piagent.ToolResult{
		Content: fmt.Sprintf("已提案 #%d（candidate，待用户在管理端确认）：%s", entry.ID, statement),
	}, nil
}

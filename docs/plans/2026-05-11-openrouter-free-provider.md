# OpenRouter Free Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `user:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Go 网关中集成 OpenRouter，启动时自动拉取免费模型列表并构建 fallback 链，用户通过 `/model free` 即可使用当前最优免费模型。

**Architecture:** 新增 `internal/provider/openrouter.go`，实现 `OpenRouterProvider`，复用 `OpenAIProvider` 的请求/响应转换逻辑，在构造时异步拉取免费模型列表并缓存到本地文件（6小时有效期）；`main.go` 读取缓存后动态调用 `RegisterChain` 构建 fallback 链，无需硬编码模型名。

**Tech Stack:** Go 1.21+, `net/http`, `encoding/json`，无新外部依赖

---

## File Map

| 操作 | 文件 | 说明 |
|------|------|------|
| Create | `go-gateway/internal/provider/openrouter.go` | OpenRouterProvider 实现 + 模型列表拉取/缓存逻辑 |
| Modify | `go-gateway/internal/config/config.go` | 新增 `OpenRouterAPIKey` 字段 |
| Modify | `go-gateway/main.go` | 注册 OpenRouter provider + 动态构建 free chain |
| Modify | `go-gateway/internal/provider/router.go` | `mapModelName` 加 `free` 透传逻辑 |
| Modify | `go-gateway/.env` | 新增 `OPENROUTER_API_KEY` |
| Modify | `go-gateway/.env.example` | 同步更新示例 |

---

## Task 1：新增 Config 字段

**Files:**
- Modify: `go-gateway/internal/config/config.go`

- [ ] **Step 1：在 `Config` 结构体添加字段**

在 `EasyClawAPIKey` 下方加一行：

```go
type Config struct {
    Port            int
    LogLevel        string
    MasterKey       string
    GLMAPIKey       string
    MIMOAPIKey      string
    LongcatAPIKey   string
    EasyClawAPIKey  string
    OpenRouterAPIKey string // OpenRouter 免费模型网关
    Env             string
}
```

- [ ] **Step 2：在 `Load()` 函数中读取环境变量**

在 `EasyClawAPIKey: getEnv(...)` 下方加一行：

```go
OpenRouterAPIKey: getEnv("OPENROUTER_API_KEY", ""),
```

- [ ] **Step 3：编译验证**

```bash
cd go-gateway && go build ./... 2>&1
```

期望输出：无错误

- [ ] **Step 4：Commit**

```bash
git add go-gateway/internal/config/config.go
git commit -m "feat: add OpenRouterAPIKey config field"
```

---

## Task 2：实现 OpenRouterProvider

**Files:**
- Create: `go-gateway/internal/provider/openrouter.go`

### 背景知识

OpenRouter 的接口与 OpenAI 完全兼容，差异只有两点：
1. 额外需要 `HTTP-Referer` 和 `X-Title` 请求头（用于 App Activity 追踪，不加也能工作）
2. 免费模型 ID 格式为 `provider/model-name:free`，如 `qwen/qwen3-coder:free`

模型列表 API：`GET https://openrouter.ai/api/v1/models`
聊天 API：`POST https://openrouter.ai/api/v1/chat/completions`

免费模型判断条件：`pricing.prompt == "0"` 或 model ID 包含 `:free`

### 实现

- [ ] **Step 1：创建 `openrouter.go`**

```go
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	openRouterChatURL   = "https://openrouter.ai/api/v1/chat/completions"
	openRouterCacheTTL  = 6 * time.Hour
)

// openRouterModel 是 OpenRouter /models 接口返回的单个模型
type openRouterModel struct {
	ID            string                 `json:"id"`
	ContextLength int                    `json:"context_length"`
	Pricing       map[string]interface{} `json:"pricing"`
	Created       int64                  `json:"created"`
}

// IsFree 判断模型是否免费
func (m *openRouterModel) IsFree() bool {
	if strings.Contains(m.ID, ":free") {
		return true
	}
	if p, ok := m.Pricing["prompt"]; ok {
		switch v := p.(type) {
		case string:
			return v == "0"
		case float64:
			return v == 0
		}
	}
	return false
}

// openRouterCache 本地缓存结构
type openRouterCache struct {
	FetchedAt time.Time         `json:"fetched_at"`
	Models    []openRouterModel `json:"models"`
}

// OpenRouterProvider 实现了 Provider 和 StreamProvider 接口。
// 请求/响应转换完全复用 OpenAIProvider 的逻辑（组合而非继承）。
type OpenRouterProvider struct {
	inner  *OpenAIProvider // 复用 OpenAI 格式转换
	apiKey string
}

// NewOpenRouterProvider 构造一个 OpenRouterProvider。
// name 是此实例在 Router 中的标识（如 "openrouter-free-0"）。
// model 是要发给 OpenRouter 的实际模型 ID（如 "qwen/qwen3-coder:free"）。
func NewOpenRouterProvider(name, model, apiKey string) *OpenRouterProvider {
	return &OpenRouterProvider{
		inner: NewOpenAIProvider(&Config{
			Name:   name,
			URL:    openRouterChatURL,
			APIKey: apiKey,
		}),
		apiKey: apiKey,
	}
}

// 实现 Provider 接口（全部委托给 inner）
func (p *OpenRouterProvider) Name() string    { return p.inner.Name() }
func (p *OpenRouterProvider) URL() string     { return p.inner.URL() }
func (p *OpenRouterProvider) APIKey() string  { return p.inner.APIKey() }
func (p *OpenRouterProvider) UseBearer() bool { return true }

func (p *OpenRouterProvider) ForwardRequest(ctx context.Context, req *Request) (*Response, error) {
	return p.inner.ForwardRequest(ctx, req)
}

func (p *OpenRouterProvider) IsHealthy(ctx context.Context) bool {
	return true
}

// 实现 StreamProvider 接口
func (p *OpenRouterProvider) ForwardStream(ctx context.Context, req *Request, w io.Writer) error {
	return p.inner.ForwardStream(ctx, req, req, w)
}

// ─── 免费模型列表拉取 ──────────────────────────────────────────────────────────

// FetchFreeModels 拉取 OpenRouter 免费模型列表，优先读缓存。
// 按 context_length 降序排列，返回前 n 个。
func FetchFreeModels(apiKey string, n int) ([]openRouterModel, error) {
	cached, err := loadCache()
	if err == nil && time.Since(cached.FetchedAt) < openRouterCacheTTL {
		return topN(cached.Models, n), nil
	}

	models, err := fetchFromAPI(apiKey)
	if err != nil {
		// 拉取失败时降级使用过期缓存
		if cached != nil {
			return topN(cached.Models, n), nil
		}
		return nil, err
	}

	_ = saveCache(&openRouterCache{FetchedAt: time.Now(), Models: models})
	return topN(models, n), nil
}

func fetchFromAPI(apiKey string) ([]openRouterModel, error) {
	req, err := http.NewRequest(http.MethodGet, openRouterModelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter models fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter models HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openrouter models decode: %w", err)
	}

	var free []openRouterModel
	for _, m := range result.Data {
		if m.IsFree() {
			free = append(free, m)
		}
	}

	// 按 context_length 降序
	sort.Slice(free, func(i, j int) bool {
		return free[i].ContextLength > free[j].ContextLength
	})
	return free, nil
}

func cacheFilePath() string {
	dir, _ := os.UserCacheDir()
	return filepath.Join(dir, "go-llm-gateway", "openrouter-models.json")
}

func loadCache() (*openRouterCache, error) {
	data, err := os.ReadFile(cacheFilePath())
	if err != nil {
		return nil, err
	}
	var c openRouterCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveCache(c *openRouterCache) error {
	path := cacheFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(c)
	return os.WriteFile(path, data, 0o644)
}

func topN(models []openRouterModel, n int) []openRouterModel {
	if n <= 0 || n >= len(models) {
		return models
	}
	return models[:n]
}
```

- [ ] **Step 2：添加缺失的 import**

文件顶部 import 块需要：

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)
```

- [ ] **Step 3：修复 ForwardStream 签名（编译错误预防）**

`inner.ForwardStream` 签名是 `(ctx, req, w)`，上面有个笔误，改为：

```go
func (p *OpenRouterProvider) ForwardStream(ctx context.Context, req *Request, w io.Writer) error {
	return p.inner.ForwardStream(ctx, req, w)
}
```

- [ ] **Step 4：编译验证**

```bash
cd go-gateway && go build ./... 2>&1
```

期望输出：无错误

- [ ] **Step 5：Commit**

```bash
git add go-gateway/internal/provider/openrouter.go
git commit -m "feat: add OpenRouterProvider with free model list fetching"
```

---

## Task 3：main.go 动态注册 OpenRouter Provider

**Files:**
- Modify: `go-gateway/main.go`

### 设计说明

启动时调用 `FetchFreeModels`（最多拉取 top 5），为每个模型创建一个独立的 `OpenRouterProvider` 实例并注册，然后将它们全部加入 `free` chain 形成 fallback 链：

```
free → [openrouter-free-0(qwen/qwen3-coder:free), openrouter-free-1(google/gemma:free), ...]
```

每个 Provider 实例在构造时已绑定好实际模型名，`mapModelName` 不需要做映射（透传即可）。

- [ ] **Step 1：在注册 EasyClaw 之后、Chain 注册之前加入以下代码**

```go
// OpenRouter 免费模型（启动时动态拉取，fallback 链自动构建）
var openRouterChain []string
if cfg.OpenRouterAPIKey != "" {
    freeModels, err := provider.FetchFreeModels(cfg.OpenRouterAPIKey, 5)
    if err != nil {
        logger.Printf("Warning: failed to fetch OpenRouter free models: %v", err)
    } else {
        logger.Printf("OpenRouter: loaded %d free models", len(freeModels))
        for i, m := range freeModels {
            name := fmt.Sprintf("openrouter-free-%d", i)
            router.RegisterProvider(name, provider.NewOpenRouterProvider(name, m.ID, cfg.OpenRouterAPIKey))
            openRouterChain = append(openRouterChain, name)
            logger.Printf("  [%d] %s (ctx=%dK)", i, m.ID, m.ContextLength/1000)
        }
    }
}
```

- [ ] **Step 2：在 Chain 注册区加入 free chain**

```go
if len(openRouterChain) > 0 {
    router.RegisterChain("free", openRouterChain)
    router.RegisterChain("openrouter-free", openRouterChain)
}
```

- [ ] **Step 3：确保 `fmt` 在 import 列表中**

`main.go` 顶部的 import 已有 `"fmt"`，无需改动。

- [ ] **Step 4：编译验证**

```bash
cd go-gateway && go build ./... 2>&1
```

期望输出：无错误

- [ ] **Step 5：Commit**

```bash
git add go-gateway/main.go
git commit -m "feat: dynamically register OpenRouter free model providers at startup"
```

---

## Task 4：router.go mapModelName 透传

**Files:**
- Modify: `go-gateway/internal/provider/router.go`

### 说明

`openrouter-free-0` 这类 Provider 的模型名已在构造时绑定（Provider 实例本身知道自己要调哪个模型），`MapModel` 返回什么就透传给上游。

但当前 `mapModelName` 对未知 key 直接返回原始 `modelName`（即 `free`），这会把 `free` 这个字符串发给 OpenRouter——这是错的。

需要在 `ForwardRequest`/`ForwardStream` 时使用 Provider 自身绑定的模型名，而不是 chain 的别名。

实现方式：给 `OpenRouterProvider` 增加一个 `ModelID()` 方法，在 handler 路由时优先使用 provider 自身的模型名。

- [ ] **Step 1：在 `openrouter.go` 中给 `OpenRouterProvider` 添加 `modelID` 字段和方法**

```go
type OpenRouterProvider struct {
	inner   *OpenAIProvider
	apiKey  string
	modelID string // 绑定的真实模型 ID，如 "qwen/qwen3-coder:free"
}

func NewOpenRouterProvider(name, model, apiKey string) *OpenRouterProvider {
	return &OpenRouterProvider{
		inner: NewOpenAIProvider(&Config{
			Name:   name,
			URL:    openRouterChatURL,
			APIKey: apiKey,
		}),
		apiKey:  apiKey,
		modelID: model,
	}
}

// BoundModel 返回此 Provider 绑定的真实模型 ID
func (p *OpenRouterProvider) BoundModel() string { return p.modelID }
```

- [ ] **Step 2：在 `types.go` 中定义 `BoundModelProvider` 接口**

在 `StreamProvider` 接口定义之后加：

```go
// BoundModelProvider 是可选接口：Provider 自身绑定了特定模型名。
// handler 在路由时检查此接口，若实现则直接使用 BoundModel() 替代 MapModel() 的结果。
type BoundModelProvider interface {
    Provider
    BoundModel() string
}
```

- [ ] **Step 3：在 `messages.go` 的 `handleStream` 中使用 BoundModel**

找到这段代码：

```go
req.Model = h.router.MapModel(originalModel, p.Name())
```

改为：

```go
if bmp, ok := p.(provider.BoundModelProvider); ok {
    req.Model = bmp.BoundModel()
} else {
    req.Model = h.router.MapModel(originalModel, p.Name())
}
```

- [ ] **Step 4：在 `messages.go` 的 `handleNonStream` 中同样处理**

找到 `Forward` 调用之前，`req.Model` 是用 chain 别名传入 `router.Forward` 的，`router.Forward` 内部会调 `mapModelName`——此处不影响 `openrouter-free-*` provider，因为 `mapModelName` 对这些 key 无匹配会原样返回 provider name，但实际模型名需要正确。

在 `router.Forward` 内部 `Forward` 循环里同样加 `BoundModelProvider` 判断：

在 `router.go` 的 `Forward` 方法中，找到：

```go
req.Model = r.mapModelName(modelName, p.Name())
```

改为：

```go
if bmp, ok := p.(BoundModelProvider); ok {
    req.Model = bmp.BoundModel()
} else {
    req.Model = r.mapModelName(modelName, p.Name())
}
```

- [ ] **Step 5：编译验证**

```bash
cd go-gateway && go build ./... 2>&1
```

期望输出：无错误

- [ ] **Step 6：Commit**

```bash
git add go-gateway/internal/provider/openrouter.go \
        go-gateway/internal/provider/types.go \
        go-gateway/internal/handlers/messages.go \
        go-gateway/internal/provider/router.go
git commit -m "feat: add BoundModelProvider interface for dynamic model binding"
```

---

## Task 5：更新 .env 和 .env.example

**Files:**
- Modify: `go-gateway/.env`
- Modify: `go-gateway/.env.example`

- [ ] **Step 1：在 `.env` 的 `EASYCLAW_API_KEY` 行之后加入**

```
OPENROUTER_API_KEY=sk-or-v1-xxxxxx  # 从 openrouter.ai/keys 获取，免费注册
```

- [ ] **Step 2：在 `.env.example` 同样位置加入相同格式的示例行**

- [ ] **Step 3：Commit**

```bash
git add go-gateway/.env.example
git commit -m "docs: add OPENROUTER_API_KEY to env example"
```

注意：`.env` 本身不进 git（已在 `.gitignore`），只 commit `.env.example`。

---

## Task 6：集成测试

**Files:** 无新文件，手动验证

- [ ] **Step 1：填入真实 key，重启网关**

在 `go-gateway/.env` 中填入 OpenRouter key，然后：

```bash
cd go-gateway
lsof -ti:4001 | xargs kill -9 2>/dev/null
./gateway > /tmp/go-gateway.log 2>&1 &
sleep 2
```

- [ ] **Step 2：观察启动日志，确认模型列表加载成功**

```bash
cat /tmp/go-gateway.log | head -20
```

期望输出包含：
```
OpenRouter: loaded 5 free models
  [0] qwen/qwen3-coder:free (ctx=262K)
  [1] google/gemma-4-31b-it:free (ctx=262K)
  ...
```

- [ ] **Step 3：非流式测试**

```bash
curl -s -X POST http://localhost:4001/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-hwj123hwj" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "free",
    "max_tokens": 50,
    "messages": [{"role":"user","content":[{"type":"text","text":"hello，一句话回复"}]}],
    "stream": false
  }' | python3 -m json.tool
```

期望：返回 `type: message`，`content[0].type: text`，`stop_reason: end_turn`

- [ ] **Step 4：流式测试**

```bash
curl -s -X POST http://localhost:4001/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-hwj123hwj" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "free",
    "max_tokens": 50,
    "messages": [{"role":"user","content":[{"type":"text","text":"hello"}]}],
    "stream": true
  }'
```

期望：看到 `event: message_start` → `content_block_start` → `content_block_delta` → `message_stop` 的 SSE 序列

- [ ] **Step 5：缓存验证**

```bash
ls -la ~/Library/Caches/go-llm-gateway/openrouter-models.json
cat ~/Library/Caches/go-llm-gateway/openrouter-models.json | python3 -m json.tool | head -10
```

期望：文件存在，内容包含 `fetched_at` 和 `models` 数组

- [ ] **Step 6：最终 commit**

```bash
git add go-gateway/.env
git commit -m "feat: integrate OpenRouter free model provider with dynamic chain"
```

---

## 附录：后续扩展说明

### 增加更多免费 Provider

只需在 `main.go` 仿照 OpenRouter 的模式注册新 `OpenAIProvider`，无需修改任何其他文件：

```go
// 示例：接入 Groq 免费额度
router.RegisterProvider("groq-free", provider.NewOpenAIProvider(&provider.Config{
    Name:   "groq-free",
    URL:    "https://api.groq.com/openai/v1/chat/completions",
    APIKey: cfg.GroqAPIKey,
}))
router.RegisterChain("free", append(openRouterChain, "groq-free"))
```

### Free Chain 自动刷新

当前缓存 6 小时后下次启动才更新。若需运行时刷新，可在网关添加一个管理端点：

```
POST /admin/refresh-free-models
```

调用 `FetchFreeModels(key, 5, forceRefresh=true)` 后重新 `RegisterChain`。这是可选优化，当前方案已满足主要需求。

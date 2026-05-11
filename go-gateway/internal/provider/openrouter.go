package provider

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
	inner   *OpenAIProvider // 复用 OpenAI 格式转换
	apiKey  string
	modelID string // 绑定的真实模型 ID，如 "qwen/qwen3-coder:free"
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
		apiKey:  apiKey,
		modelID: model,
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

// BoundModel 返回此 Provider 绑定的真实模型 ID
func (p *OpenRouterProvider) BoundModel() string { return p.modelID }

// ForwardStream 实现 StreamProvider 接口
func (p *OpenRouterProvider) ForwardStream(ctx context.Context, req *Request, w io.Writer) error {
	return p.inner.ForwardStream(ctx, req, w)
}

// ─── 免费模型列表拉取 ──────────────────────────────────────────────────────────

// FetchFreeModels 拉取 OpenRouter 免费模型列表，优先读缓存。
// 按 context_length 降序排列，返回前 n 个。
func FetchFreeModels(apiKey string, n int) ([]openRouterModel, error) {
	cached, err := loadOpenRouterCache()
	if err == nil && time.Since(cached.FetchedAt) < openRouterCacheTTL {
		return topNModels(cached.Models, n), nil
	}

	models, err := fetchOpenRouterFromAPI(apiKey)
	if err != nil {
		// 拉取失败时降级使用过期缓存
		if cached != nil {
			return topNModels(cached.Models, n), nil
		}
		return nil, err
	}

	_ = saveOpenRouterCache(&openRouterCache{FetchedAt: time.Now(), Models: models})
	return topNModels(models, n), nil
}

func fetchOpenRouterFromAPI(apiKey string) ([]openRouterModel, error) {
	req, err := http.NewRequest(http.MethodGet, openRouterModelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/weijian/go-llm-gateway")
	req.Header.Set("X-Title", "go-llm-gateway")

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

func openRouterCacheFilePath() string {
	dir, _ := os.UserCacheDir()
	return filepath.Join(dir, "go-llm-gateway", "openrouter-models.json")
}

func loadOpenRouterCache() (*openRouterCache, error) {
	data, err := os.ReadFile(openRouterCacheFilePath())
	if err != nil {
		return nil, err
	}
	var c openRouterCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveOpenRouterCache(c *openRouterCache) error {
	path := openRouterCacheFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(c)
	return os.WriteFile(path, data, 0o644)
}

func topNModels(models []openRouterModel, n int) []openRouterModel {
	if n <= 0 || n >= len(models) {
		return models
	}
	return models[:n]
}

// ModelAlias 从 OpenRouter 模型 ID 中提取简短别名，用于 /model 切换。
// 例：
//   "qwen/qwen3-coder:free"                    → "qwen3-coder"
//   "nvidia/nemotron-3-super-120b-a12b:free"   → "nemotron"
//   "google/gemma-4-31b-it:free"               → "gemma"
//   "openrouter/owl-alpha"                     → "owl-alpha"
func ModelAlias(modelID string) string {
	// 去掉 :free 后缀
	id := strings.TrimSuffix(modelID, ":free")
	// 取最后一个 / 之后的部分
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		id = id[idx+1:]
	}
	// 取第一个 - 分隔的词（品牌名），长度 > 2 才有意义
	parts := strings.SplitN(id, "-", 2)
	alias := parts[0]
	if len(alias) <= 2 {
		return id // 太短就用完整名
	}
	return alias
}

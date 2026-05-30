# 技术指南（面向 Agent）

本文件供 AI 助手阅读，描述项目的完整技术架构、各组件职责、启动方式和扩展方法。

---

## 项目概述

一个 LLM API 网关，让 Claude Code 通过统一的 Anthropic 兼容接口接入多家国产 AI 提供商。

**支持的提供商：**
- 智谱 BigModel（GLM）— Anthropic 兼容格式
- 小米 MiMo — Anthropic 兼容格式（带思考块）
- 美团 LongCat — Anthropic 兼容格式（需要 Bearer token）
- EasyClaw — OpenAI 格式（需要格式转换）
- GitHub Copilot — OpenAI 兼容格式（Gemini 模型为主）

**两套实现：**
1. `go-gateway/` — Go 实现，18 MB 内存，推荐
2. `litellm/` + `docker-compose.yaml` — Python LiteLLM 实现，~570 MB 内存

---

## Go 网关

### 目录结构

```
go-gateway/
├── main.go                        # 入口：初始化、注册 provider、启动 Gin
├── .env                           # 实际使用的 env（不提交 git）
├── .env.example                   # env 模板
├── internal/
│   ├── config/
│   │   └── config.go              # 从 .env 加载配置，验证必填项
│   ├── provider/
│   │   ├── types.go               # 公共类型：Request, Response, Provider 接口, StreamProvider 接口
│   │   ├── anthropic.go           # Anthropic 兼容 provider（GLM/MiMo/LongCat）
│   │   ├── openai.go              # OpenAI 格式 provider（EasyClaw），含格式转换
│   │   ├── copilot.go             # GitHub Copilot provider（Gemini 模型为主）
│   │   └── router.go              # 路由器：provider 注册、model→provider 映射、fallback
│   ├── handlers/
│   │   ├── messages.go            # POST /v1/messages，支持流式和非流式
│   │   ├── models.go              # GET /v1/models
│   │   └── health.go              # GET /health（无需认证）
│   ├── auth/
│   │   └── auth.go                # Bearer token 中间件，/health 豁免
│   └── middleware/
│       └── logging.go             # 请求日志：METHOD PATH -> STATUS (duration)
```

### 关键设计

**Provider 接口**（`internal/provider/types.go`）：
```go
type Provider interface {
    Name() string
    URL() string
    APIKey() string
    UseBearer() bool
    ForwardRequest(ctx context.Context, req *Request) (*Response, error)
    IsHealthy(ctx context.Context) bool
}

// 可选接口，实现后 handler 走格式转换路径而非直接透传 SSE
type StreamProvider interface {
    Provider
    ForwardStream(ctx context.Context, req *Request, w io.Writer) error
}
```

**流式请求处理逻辑**（`internal/handlers/messages.go`）：
- Anthropic 兼容 provider → 直接透传上游 SSE 流
- `StreamProvider` 实现方（如 `OpenAIProvider`）→ 调 `ForwardStream`，由 provider 自己做格式转换

**OpenAI → Anthropic 格式转换**（`internal/provider/openai.go`）：

| 方向 | 转换内容 |
|------|---------|
| 请求（入） | 字段基本一致，直接映射 |
| 响应（出，非流式） | `choices[0].message.content` → `content[{type,text}]`；`finish_reason:stop` → `stop_reason:end_turn`；token 字段重命名 |
| 响应（出，流式） | 生成 Anthropic SSE 事件序列（`message_start` → `content_block_delta`... → `message_stop`） |

**Model 映射**（`internal/provider/router.go` 的 `mapModelName`）：
```
glm-sonnet  → glm-5-turbo（发给智谱）
glm-opus    → glm-5.1
mimo-opus   → mimo-v2.5-pro
longcat-sonnet → LongCat-Flash-Chat
easyclaw-sonnet → claude-sonnet-4-6（发给 EasyClaw）
coding      → glm-5-turbo / mimo-v2.5 / LongCat-Flash-Chat（fallback 链）
glm-flash   → glm-4.7-flash（智谱免费，复用 GLM_API_KEY）
free        → OpenRouter top-5 免费模型 fallback 链（启动时动态构建）
copilot-opus → gemini-3.1-pro-preview（GitHub Copilot，免费教育套餐）
copilot-sonnet → gemini-3-flash-preview（GitHub Copilot，免费教育套餐）
copilot-haiku → gpt-4o-2024-11-20（GitHub Copilot，免费教育套餐）
```

> **特别说明**：OpenRouter 免费模型由 `OpenRouterProvider` 实现 `BoundModelProvider` 接口，模型名已在构造时绑定，不过 `mapModelName`。每个免费模型的简称（如 `nemotron`、`owl`）在启动时动态注册为独立 chain。

> **Copilot 说明**：GitHub Copilot 使用 OAuth device code flow 认证，token 有效期约 30 分钟。代码支持自动刷新（需要 `COPILOT_GITHUB_TOKEN`）。免费教育套餐（`free_educational_quota`）可用的模型：Gemini 3.1 Pro Preview、Gemini 3 Flash Preview、GPT-4o 等。Codex 模型不支持 `/chat/completions` 端点。

### 环境变量

| 变量 | 必填 | 说明 |
|------|------|------|
| `LITELLM_MASTER_KEY` | 是 | 网关认证 token，Claude Code 用此 token 调用网关 |
| `GLM_API_KEY` | 否 | 智谱 API key，缺失则不注册 GLM provider |
| `MIMO_API_KEY` | 否 | 小米 API key |
| `LONGCAT_API_KEY` | 否 | 美团 API key |
| `EASYCLAW_API_KEY` | 否 | EasyClaw API key |
| `OPENROUTER_API_KEY` | 否 | OpenRouter key，启用免费模型（`/model free`），启动时自动拉取模型列表 |
| `COPILOT_TOKEN` | 否 | GitHub Copilot token（短期有效，约 30 分钟） |
| `COPILOT_GITHUB_TOKEN` | 否 | GitHub OAuth token（用于自动刷新 Copilot token） |
| `PORT` | 否 | 监听端口，默认 4000 |
| `LOG_LEVEL` | 否 | 日志级别，默认 info |

### 启动

```bash
cd go-gateway
go build -o gateway . && ./gateway
```

或使用后台方式：
```bash
./gateway > /tmp/go-gateway.log 2>&1 &
```

### 新增 Provider 步骤

1. 在 `internal/provider/` 下新建文件（如 `baidu.go`）
2. 实现 `Provider` 接口（参考 `anthropic.go`）
3. 如果该 provider 是 OpenAI 格式，还需实现 `StreamProvider`（参考 `openai.go`）
4. 在 `internal/config/config.go` 的 `Config` 结构体加新字段（如 `BaiduAPIKey`）
5. 在 `main.go` 注册 provider 和 fallback chain
6. 在 `router.go` 的 `mapModelName` 加模型名映射

---

## LiteLLM 网关

### 核心文件

| 文件 | 职责 |
|------|------|
| `litellm/config.yaml` | 模型路由：定义每个 model_name 对应哪个 provider 的哪个模型 |
| `litellm/longcat_auth.py` | 美团认证回调：把 `x-api-key` 重写为 `Authorization: Bearer` |
| `~/.litellm/.env` | API keys（不在仓库中） |
| `docker-compose.yaml` | 完整栈（LiteLLM + PostgreSQL） |
| `scripts/start-local-no-db.sh` | 仅启动 LiteLLM，无数据库 |

### 关键配置细节

**EasyClaw** 的 `config.yaml` 配置：
```yaml
- model_name: claude-sonnet-4-6
  litellm_params:
    model: openai/claude-sonnet-4-6   # 前缀 openai/ 告诉 LiteLLM 用 OpenAI 格式
    api_base: https://api.easyclaw.work
    api_key: os.environ/EASYCLAW_API_KEY
    drop_params: true                  # 丢弃 Anthropic 独有字段（thinking 等）
```

**美团认证问题**：LiteLLM 默认用 `x-api-key` header，但美团只接受 `Authorization: Bearer`。
解决：通过 `async_pre_call_hook` 拦截请求重写 header（见 `litellm/longcat_auth.py`）。

### 启动（无数据库）

```bash
./scripts/start-local-no-db.sh
```

监听 `:4000`。

### 新增 Provider 步骤（LiteLLM）

1. 在 `~/.litellm/.env` 加新的 `XXX_API_KEY`
2. 在 `litellm/config.yaml` 的 `model_list` 加配置块
3. 若 API 使用 Bearer token 认证，在 `longcat_auth.py` 添加同类逻辑
4. 重启容器（`docker restart litellm`）

---

## Claude Code 接入

编辑 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4001/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-local-gateway-xxx"
  }
}
```

- Go 网关用端口 `4001`
- LiteLLM 网关用端口 `4000`
- `ANTHROPIC_AUTH_TOKEN` 的值是 `.env` 中的 `LITELLM_MASTER_KEY`

---

## 常见问题

**Q: Go 网关显示 `address already in use`**
```bash
pkill -f "go-gateway/gateway"
./gateway > /tmp/go-gateway.log 2>&1 &
```

**Q: MiMo 响应只有 `thinking` 块，没有 `text`**
MiMo 是思考模型，`max_tokens` 太小时 token 被思考过程占满。调大 `max_tokens` 到 200+ 即可。

**Q: LiteLLM 一直报 DB 连接错误**
正常现象，无数据库模式下 LiteLLM 会反复尝试重连 PostgreSQL 但不影响 API 功能。若想消除日志，用完整的 `docker-compose.yaml` 启动。

**Q: 想添加流式支持给 OpenAI 格式的新 provider**
参考 `go-gateway/internal/provider/openai.go` 的 `ForwardStream` 方法，实现 `StreamProvider` 接口即可，handler 会自动识别。

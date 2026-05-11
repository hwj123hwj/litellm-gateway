# go-gateway

轻量级 LLM API 网关，用 Go 编写。支持智谱、小米、美团、EasyClaw 四家提供商，内存占用约 18 MB。

## 快速启动

### 前提条件

- Go 1.21+（编译时需要，运行时不需要）
- 至少一个提供商的 API key

### 1. 配置 .env

```bash
cp .env.example .env
# 编辑 .env，填入你的 API keys
```

`.env` 最小配置：

```env
LITELLM_MASTER_KEY=sk-local-gateway-xxx   # 必填，网关认证 token
GLM_API_KEY=your_glm_key                  # 至少填一个 provider
PORT=4001
```

### 2. 编译并运行

```bash
go build -o gateway . && ./gateway
```

或使用 Makefile：

```bash
make run
```

### 3. 配置 Claude Code

编辑 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4001/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-local-gateway-xxx"
  }
}
```

---

## API 端点

| 端点 | 方法 | 认证 | 说明 |
|------|------|------|------|
| `/health` | GET | 无需 | 健康检查 |
| `/v1/models` | GET | Bearer | 列出可用模型 |
| `/v1/messages` | POST | Bearer | 发送消息（支持流式） |

### 健康检查

```bash
curl http://localhost:4001/health
# {"status":"ok"}
```

### 发送消息（非流式）

```bash
curl -X POST http://localhost:4001/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-sonnet",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

### 发送消息（流式）

```bash
curl -X POST http://localhost:4001/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-sonnet",
    "max_tokens": 100,
    "stream": true,
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

---

## 可用模型

### 智谱 BigModel

| 模型名 | 实际模型 | 说明 |
|--------|---------|------|
| `glm-haiku` | glm-4.7 | 轻量快速 |
| `glm-sonnet` | glm-5-turbo | 主力模型 |
| `glm-opus` | glm-5.1 | 旗舰模型 |

### 小米 MiMo

| 模型名 | 实际模型 | 说明 |
|--------|---------|------|
| `mimo-haiku` | mimo-v2.5 | 思考模型 |
| `mimo-sonnet` | mimo-v2.5 | 思考模型 |
| `mimo-opus` | mimo-v2.5-pro | 思考旗舰 |

> **注意**：MiMo 是思考模型，响应中包含 `thinking` 类型的内容块。`max_tokens` 需设置足够大（建议 200+），否则 token 可能被思考过程耗尽。

### 美团 LongCat

| 模型名 | 实际模型 | 说明 |
|--------|---------|------|
| `longcat-sonnet` | LongCat-Flash-Chat | 长上下文 |
| `longcat-opus` | LongCat-2.0-Preview | 长上下文旗舰 |

### EasyClaw（真实 Claude）

| 模型名 | 实际模型 | 说明 |
|--------|---------|------|
| `easyclaw-sonnet` | claude-sonnet-4-6 | 真实 Claude |
| `easyclaw-opus` | claude-opus-4-6 | 真实 Claude 旗舰 |
| `claude-sonnet-4-6` | claude-sonnet-4-6 | 兼容别名 |

> EasyClaw 使用 OpenAI `/v1/chat/completions` 格式，网关会自动做格式转换（无需手动处理）。

### Fallback 链

| 模型名 | Fallback 顺序 | 说明 |
|--------|-------------|------|
| `coding` | GLM → MiMo → LongCat | **日常推荐**，自动容错 |

---

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `LITELLM_MASTER_KEY` | 是 | — | 网关认证 token |
| `GLM_API_KEY` | 否 | — | 智谱 API key |
| `MIMO_API_KEY` | 否 | — | 小米 API key |
| `LONGCAT_API_KEY` | 否 | — | 美团 API key |
| `EASYCLAW_API_KEY` | 否 | — | EasyClaw API key |
| `PORT` | 否 | 4000 | 监听端口 |
| `LOG_LEVEL` | 否 | info | 日志级别 |

未配置 key 的 provider 会被自动跳过，不影响其他 provider 正常工作。

---

## 架构

```
Claude Code
    │ Anthropic API 格式
    ▼
┌─────────────────────────────┐
│        Go Gateway           │
│                             │
│  Auth Middleware            │
│  Logging Middleware         │
│                             │
│  Router                     │
│  ┌──────────────────────┐   │
│  │ model → provider 映射│   │
│  │ fallback 链管理       │   │
│  └──────────────────────┘   │
│                             │
│  Providers                  │
│  ├── AnthropicProvider      │──▶ GLM / MiMo / LongCat
│  └── OpenAIProvider         │──▶ EasyClaw（含格式转换）
└─────────────────────────────┘
```

**核心设计**：`Provider` 接口 + 可选 `StreamProvider` 接口。

- `AnthropicProvider`：直接透传 SSE 流
- `OpenAIProvider`：实现 `StreamProvider`，把 OpenAI SSE 转为 Anthropic SSE

Handler 通过 Go interface type assertion 自动区分，调用方无感知。

---

## 开发

### 运行测试

```bash
make test          # 单元测试
go test -v ./...   # 详细输出
```

### 常用命令

```bash
make build         # 编译
make run           # 编译并运行
make test          # 测试
make fmt           # 格式化代码
make docker-build  # 构建 Docker 镜像
make docker-run    # Docker Compose 启动
```

### 新增 Provider

1. 在 `internal/provider/` 新建文件（如 `baidu.go`）
2. 实现 `Provider` 接口（参考 `anthropic.go`）
3. 如果是 OpenAI 格式，实现 `StreamProvider`（参考 `openai.go`）
4. 在 `internal/config/config.go` 加新字段
5. 在 `main.go` 注册 provider 和 chain
6. 在 `router.go` 的 `mapModelName` 加模型映射

---

## Docker 部署

```bash
# 构建镜像
docker build -t go-llm-gateway .

# 运行（传入 .env 文件）
docker run -d \
  --name go-gateway \
  -p 4001:4001 \
  --env-file .env \
  go-llm-gateway

# 或用 Docker Compose
docker-compose up -d
```

---

## 资源对比

| | Go 网关 | LiteLLM |
|--|--|--|
| 内存 | ~18 MB | ~570 MB |
| 启动时间 | <1 秒 | ~15 秒 |
| 二进制大小 | ~15 MB | — |
| Docker 镜像 | ~50 MB | ~711 MB |

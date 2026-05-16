# LLM Gateway

让 Claude Code 统一接入多家国产 AI 模型的网关。支持智谱、小米、美团、EasyClaw 四家提供商，支持 OpenAI 与 Anthropic 两套对外接口，并支持自动 fallback。

## 两套方案

本项目提供两套可独立运行的网关实现，按需选择：

| | Go 网关（推荐） | LiteLLM 网关 |
|--|--|--|
| **内存占用** | ~18 MB | ~570 MB |
| **启动时间** | <1 秒 | ~15 秒 |
| **依赖** | 无（单二进制） | Docker + Python |
| **维护** | 本项目自维护 | 官方维护 |
| **适用场景** | 本地开发、资源受限 | 需要完整功能 |

---

## Go 网关（推荐）

### 快速启动

```bash
cd go-gateway

# 创建 .env（复制你的 API keys）
cp .env.example .env
# 编辑 .env，填入各厂商 API key

# 编译并运行
go build -o gateway . && ./gateway
```

网关默认监听 `:4001`。

> **服务器部署**: 生产环境目标地址 `http://115.190.82.67:8080`，通过 GitHub Actions 自动部署（`.github/workflows/deploy.yml`）。详见 [go-gateway/README.md](go-gateway/README.md) 部署章节。

### 对外接口

Go 网关现在同时提供两套兼容接口：

#### 1. OpenAI 兼容

- Base URL: `http://localhost:4001/v1`
- Chat Completions: `http://localhost:4001/v1/chat/completions`

推荐模型链：

- `coding`
- `glm-sonnet`
- `mimo-sonnet`
- `longcat-sonnet`

#### 2. Anthropic 兼容

- Base URL: `http://localhost:4001/v1`
- Messages: `http://localhost:4001/v1/messages`

推荐模型链：

- `coding-anthropic`
- `glm-sonnet-anthropic`
- `mimo-sonnet-anthropic`
- `longcat-sonnet-anthropic`

### 配置 Claude Code

编辑 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4001/v1",
    "ANTHROPIC_AUTH_TOKEN": "你的 LITELLM_MASTER_KEY"
  }
}
```

### 可用模型

#### OpenAI 主链

| 模型名 | 提供商 | 说明 |
|--------|--------|------|
| `coding` | GLM coding plan → MiMo → LongCat | **推荐**，自动 fallback |
| `glm-sonnet` | 智谱 GLM coding plan | 主力模型 |
| `glm-haiku` | 智谱 GLM coding plan | 轻量快速 |
| `glm-opus` | 智谱 GLM coding plan | 旗舰模型 |
| `mimo-sonnet` | 小米 MiMo OpenAI | 思考模型 |
| `mimo-opus` | 小米 MiMo OpenAI | 思考旗舰 |
| `longcat-sonnet` | 美团 LongCat OpenAI | 长上下文 |
| `longcat-opus` | 美团 LongCat OpenAI | 长上下文旗舰 |
| `easyclaw-sonnet` | EasyClaw → Claude Sonnet | 真实 Claude |
| `claude-sonnet-4-6` | EasyClaw → Claude Sonnet | 同上（兼容别名） |
| `glm-flash` | 智谱 GLM-4.7-flash | 免费，复用 GLM key |
| `free` | OpenRouter 免费模型 fallback 链 | **零成本**，启动时动态拉取 top-5 |
| `nemotron` / `owl` / ... | OpenRouter 各免费模型 | 启动时动态生成别名 |

#### Anthropic 兼容链

| 模型名 | 提供商 | 说明 |
|--------|--------|------|
| `coding-anthropic` | GLM → MiMo → LongCat | Anthropic 兼容推荐链 |
| `glm-sonnet-anthropic` | 智谱 Anthropic | 主力模型 |
| `glm-haiku-anthropic` | 智谱 Anthropic | 轻量快速 |
| `glm-opus-anthropic` | 智谱 Anthropic | 旗舰模型 |
| `mimo-sonnet-anthropic` | 小米 Anthropic | 思考模型 |
| `mimo-opus-anthropic` | 小米 Anthropic | 思考旗舰 |
| `longcat-sonnet-anthropic` | 美团 Anthropic | 长上下文 |
| `longcat-opus-anthropic` | 美团 Anthropic | 长上下文旗舰 |

### 测试

```bash
# Health check
curl http://localhost:4001/health

# OpenAI 风格
curl -X POST http://localhost:4001/v1/chat/completions \
  -H "Authorization: Bearer <LITELLM_MASTER_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"model":"coding","messages":[{"role":"user","content":"hi"}]}'

# Anthropic 风格
curl -X POST http://localhost:4001/v1/messages \
  -H "Authorization: Bearer <LITELLM_MASTER_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"model":"coding-anthropic","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

详细文档见 [go-gateway/README.md](go-gateway/README.md)。

---

## LiteLLM 网关

适合需要 Web UI 管理界面、详细日志统计、或与现有 LiteLLM 生态集成的场景。

### 快速启动

```bash
# 准备配置
mkdir -p ~/.litellm
# 把 API keys 写入 ~/.litellm/.env（参考 docs/local-deploy.md）

# 启动（无数据库，内存更少）
./scripts/start-local-no-db.sh
```

网关默认监听 `:4000`。

### 配置 Claude Code

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4000/v1",
    "ANTHROPIC_AUTH_TOKEN": "你的 LITELLM_MASTER_KEY"
  }
}
```

详细文档见 [docs/local-deploy.md](docs/local-deploy.md)。

---

## 项目结构

```text
litellm-gateway/
├── go-gateway/              # Go 网关（推荐）
│   ├── main.go              # 启动入口
│   ├── internal/
│   │   ├── config/          # 配置加载
│   │   ├── provider/        # 提供商实现（Anthropic/OpenAI 适配）
│   │   ├── handlers/        # HTTP 路由处理
│   │   ├── auth/            # Bearer token 认证
│   │   └── middleware/      # 日志中间件
│   ├── .env.example         # 环境变量模板
│   ├── Dockerfile
│   └── README.md            # Go 网关完整文档
│
├── litellm/                 # LiteLLM 配置
│   ├── config.yaml          # 模型路由配置
│   └── longcat_auth.py      # 美团 Bearer token 认证回调
│
├── scripts/
│   ├── start-local.sh       # LiteLLM 启动（含数据库）
│   └── start-local-no-db.sh # LiteLLM 启动（无数据库）
│
├── docs/                    # 文档
│   ├── local-deploy.md      # LiteLLM 本地部署指南
│   ├── server-deploy.md     # LiteLLM 服务器部署指南
│   ├── easyclaw-setup.md    # EasyClaw 接入说明
│   ├── troubleshooting.md   # 故障排查
│   └── openai-compatible-providers.md # OpenAI 兼容上游整理
│
└── docker-compose.yaml      # LiteLLM 完整栈（含 PostgreSQL）
```

---

## 文档导航

| 文档 | 说明 |
|------|------|
| [go-gateway/README.md](go-gateway/README.md) | Go 网关完整文档（架构、开发、部署） |
| [docs/openai-compatible-providers.md](docs/openai-compatible-providers.md) | 智谱 / 小米 / 美团 OpenAI 兼容接口整理 |
| [.claude/CLAUDE.md](.claude/CLAUDE.md) | 面向 agent 的技术指南 |
| [docs/local-deploy.md](docs/local-deploy.md) | LiteLLM 本地部署步骤 |
| [docs/server-deploy.md](docs/server-deploy.md) | LiteLLM 服务器部署步骤 |
| [docs/easyclaw-setup.md](docs/easyclaw-setup.md) | EasyClaw 接入配置 |
| [docs/troubleshooting.md](docs/troubleshooting.md) | 常见问题排查 |

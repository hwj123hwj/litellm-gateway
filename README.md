# LLM Gateway

轻量级 LLM API 网关，用 Go 编写。支持智谱、小米、美团、EasyClaw、DeepV 五家提供商，支持 OpenAI 与 Anthropic 两套对外接口，并支持自动 fallback。

## 快速启动

### 本地开发

```bash
cd go-gateway

# 创建 .env
cp .env.example .env
# 编辑 .env，填入 API keys

# 编译并运行
go build -o gateway . && ./gateway
```

网关默认监听 `:4001`。

### 服务器部署

通过 GitHub Actions 自动部署，推送到 `main` 分支即可触发。

## 对外接口

### OpenAI 兼容

- Base URL: `http://localhost:4001/v1`（本地）
- Chat Completions: `POST /v1/chat/completions`

### Anthropic 兼容

- Base URL: `http://localhost:4001/v1`（本地）
- Messages: `POST /v1/messages`

## 支持的模型

### DeepV Server（内部）

| 模型别名 | 实际模型 | 工具调用 |
|----------|----------|----------|
| `deepseek-flash` | `deepseek-v4-flash` | ✅ |
| `glm-5` | `glm-5` | ✅ |
| `claude-sonnet-4-6` | `claude-sonnet-4-6` | ✅ |

### 外部提供商

| 模型名 | 提供商 | 说明 |
|--------|--------|------|
| `coding` | GLM → MiMo → LongCat | **推荐**，OpenAI 风格，自动 fallback |
| `coding-anthropic` | GLM → MiMo → LongCat | Anthropic 风格 |
| `free` | OpenRouter 免费模型 | **零成本**，自动 fallback |
| `glm-sonnet` | 智谱 GLM coding plan | 主力模型 |
| `mimo-sonnet` | 小米 MiMo | 思考模型 |
| `longcat-sonnet` | 美团 LongCat | 长上下文 |

## 配置 Claude Code

编辑 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4001/v1",
    "ANTHROPIC_AUTH_TOKEN": "你的 LITELLM_MASTER_KEY"
  }
}
```

## 示例请求

### OpenAI 风格

```bash
curl -X POST http://localhost:4001/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"coding","messages":[{"role":"user","content":"hi"}]}'
```

### Anthropic 风格

```bash
curl -X POST http://localhost:4001/v1/messages \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"coding-anthropic","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

## 项目结构

```text
litellm-gateway/
├── go-gateway/              # Go 网关（主项目）
│   ├── main.go              # 启动入口
│   ├── internal/
│   │   ├── config/          # 配置加载
│   │   ├── provider/        # 提供商实现
│   │   ├── handlers/        # HTTP 路由处理
│   │   ├── auth/            # Bearer token 认证
│   │   └── middleware/      # 日志中间件
│   ├── .env.example         # 环境变量模板
│   ├── Dockerfile
│   └── README.md            # 完整文档
│
├── .github/workflows/       # GitHub Actions CI/CD
├── docs/                    # 文档
└── README.md                # 本文件
```

## 文档

| 文档 | 说明 |
|------|------|
| [go-gateway/README.md](go-gateway/README.md) | Go 网关完整文档（架构、模型列表、部署） |
| [docs/deepv-integration-plan.md](docs/deepv-integration-plan.md) | DeepV Server 接入文档 |
| [docs/openai-compatible-providers.md](docs/openai-compatible-providers.md) | 上游提供商 OpenAI 兼容接口整理 |
| [.claude/CLAUDE.md](.claude/CLAUDE.md) | 面向 agent 的技术指南 |

## 环境变量

| 变量 | 必填 | 说明 |
|------|------|------|
| `LITELLM_MASTER_KEY` | 是 | 网关认证 token |
| `GLM_API_KEY` | 否 | 智谱 API key |
| `MIMO_API_KEY` | 否 | 小米 API key |
| `LONGCAT_API_KEY` | 否 | 美团 API key |
| `EASYCLAW_API_KEY` | 否 | EasyClaw API key |
| `OPENROUTER_API_KEY` | 否 | OpenRouter key（启用免费模型） |
| `DEEPV_ENABLED` | 否 | 启用 DeepV Server（true/false） |
| `DEEPV_WORK_DIR` | 否 | DeepV 工作目录（用于获取 Git 信息） |
| `PORT` | 否 | 监听端口（默认 4000） |

## 资源占用

- 内存：~18 MB
- 启动时间：<1 秒
- Docker 镜像：~50 MB

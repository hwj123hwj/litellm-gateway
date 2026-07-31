# LLM Gateway

轻量级 LLM API 网关，用 Go 编写。支持智谱 GLM、GitHub Copilot、ChatGPT 等提供商，支持 OpenAI Chat Completions、Anthropic Messages、OpenAI Responses API 三套对外接口，并支持自动 fallback。

## 一键安装

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/hwj123hwj/litellm-gateway/main/scripts/install.sh | bash
```

### Windows (CMD)

```cmd
curl.exe -fsSL -o "%TEMP%\llm-gateway-install.bat" https://raw.githubusercontent.com/hwj123hwj/litellm-gateway/main/scripts/install.bat && call "%TEMP%\llm-gateway-install.bat"
```

### Windows (PowerShell)

```powershell
$installer = Join-Path $env:TEMP 'llm-gateway-install.ps1'; Invoke-WebRequest -UseBasicParsing https://raw.githubusercontent.com/hwj123hwj/litellm-gateway/main/scripts/install.ps1 -OutFile $installer; powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File $installer
```

安装脚本会自动完成：下载二进制 → 配置 PATH → 引导填入 API Key → 完成。
Windows 请直接在 CMD 或 PowerShell 中运行对应命令，不需要 WSL 或 Bash。配置向导可逐个启用多个供应商，API Key 会掩码输入。重复运行会自动去除 PATH 中的重复安装项，并可在保留 Master Key 和自定义变量的前提下重新配置供应商。

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

### 添加新提供商

无需修改代码！只需编辑 `providers.yaml` 文件：

```yaml
providers:
  # 添加新的 OpenAI 兼容提供商
  - name: my-provider
    type: openai  # 或 anthropic
    url: https://api.example.com/v1/chat/completions
    api_key_env: MY_PROVIDER_API_KEY  # 环境变量名
    models:
      - id: gpt-5.5
        aliases: [gpt-5]  # 模型别名
```

然后在 `.env` 中添加 API key：

```
MY_PROVIDER_API_KEY=sk-xxx
```

重启 gateway 即可使用新模型。

### 服务器部署

通过 GitHub Actions 自动部署，推送到 `main` 分支即可触发。

## 对外接口

### OpenAI 兼容

- Base URL: `http://localhost:4001/v1`（本地）
- Chat Completions: `POST /v1/chat/completions`
- Responses API: `POST /v1/responses`（Codex CLI 专用）

### Anthropic 兼容

- Base URL: `http://localhost:4001/v1`（本地）
- Messages: `POST /v1/messages`

## 支持的模型

### ChatGPT Codex（GPT-5.5，OAuth）

使用 ChatGPT Plus/Pro 订阅的 OAuth token 直接调用，不需要额外 API key。

| 模型别名 | 实际模型 | 说明 |
|----------|----------|------|
| `gpt-5.5` | gpt-5.5 | ChatGPT Plus/Pro（需 HTTP_PROXY） |
| `gpt-5.5-pro` | gpt-5.5-pro | ChatGPT Pro |
| `gpt-5.4-mini` | gpt-5.4-mini | 轻量快速 |
| `o4-mini` | o4-mini | 推理模型 |

### GitHub Copilot

| 模型别名 | 实际转发模型 | 说明 |
|----------|--------------|------|
| `copilot` / `auto` / `copilot-auto` | `auto` | GitHub Copilot Chat (自动选择模型) |
| `copilot-opus` / `copilot-sonnet` / `copilot-haiku` | `auto` | 兼容旧别名，统一转发至 `auto` |

### 阿里 DashScope / 百炼（通义千问 Qwen）

| 模型别名 | 实际模型 | 说明 |
|----------|----------|------|
| `qwen3.5-plus` / `qwen-plus` | `qwen3.5-plus` | 通义千问 3.5 Plus |
| `qwen3-coder-plus` / `qwen-coder` | `qwen3-coder-plus` | 阿里云 Coder 旗舰模型 |
| `qwen3-coder-next` | `qwen3-coder-next` | 阿里云 Coder 次世代模型 |
| `qwen3-max-2026-01-23` | `qwen3-max-2026-01-23` | Qwen3 Max 最新版本 |

### 外部提供商

| 模型名 | 提供商 | 说明 |
|--------|--------|------|
| `coding` | 智谱 GLM / 阿里 DashScope | **推荐**，OpenAI 风格，自动 fallback |
| `coding-anthropic` | 智谱 GLM / 阿里 DashScope | Anthropic 风格，自动 fallback |
| `glm-flash` | 智谱 GLM | 免费模型 |
| `glm-sonnet` | 智谱 GLM coding plan | 主力模型 |
| `glm-opus` | 智谱 GLM coding plan | 旗舰模型 |
| `qwen-plus` | 阿里 DashScope | Qwen3.5 Plus 主力模型 |
| `qwen-coder` | 阿里 DashScope | Qwen3 Coder 代码模型 |

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
├── scripts/                 # 配套工具脚本
│   └── codex-model          # Codex CLI 模型切换
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
| `DASHSCOPE_API_KEY` | 否 | 阿里 DashScope / 百炼 API key |
| `COPILOT_TOKEN` | 否 | GitHub Copilot token（短期有效，约 30 分钟） |
| `COPILOT_GITHUB_TOKEN` | 否 | GitHub OAuth token（用于自动刷新 Copilot token） |
| `HTTP_PROXY` | 否 | HTTP 代理地址（如 `http://127.0.0.1:7890`，启用 ChatGPT Codex） |
| `PORT` | 否 | 监听端口（默认 4001） |

## 资源占用

- 内存：~18 MB
- 启动时间：<1 秒
- Docker 镜像：~50 MB

## 相关项目

- [π-go](https://github.com/hwj123hwj/pi-go) — AI 编程搭档，写代码、搜知识、放音乐

## 展示页

访问 [hwj123hwj.github.io](https://hwj123hwj.github.io) 查看所有项目。

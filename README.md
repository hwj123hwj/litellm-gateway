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

启动后直接访问 <http://localhost:4001/> 即可打开内置 Dashboard。生产版本已将 Dashboard 静态资源嵌入网关二进制，不需要额外启动 Node/Vite 进程；`web/` 下的 `npm run dev` 仅用于前端开发。

## 配置 Pi

安装并启动本地网关后，执行：

```bash
llm-gateway setup pi
```

该命令会合并更新 `~/.pi/agent/models.json`，注册 `llm-gateway` 自定义 Provider 和网关模型。它不会覆盖其他 Pi Provider，也不会复制网关主密钥；Pi 会在每次请求时通过 `llm-gateway auth print-master-key` 从网关自己的 `.env` 读取认证信息。

可先预览变更，或为远程网关指定地址：

```bash
llm-gateway setup pi --dry-run
llm-gateway setup pi --endpoint https://gateway.example.com/v1
```

然后在 Pi 中运行 `/model` 并选择 `llm-gateway/coding`。

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

### ChatGPT Codex（OAuth，可选）

使用 ChatGPT Plus/Pro 订阅的 OAuth token 直接调用，不需要额外 API key。

配置 `HTTP_PROXY` 且本机存在 Codex OAuth token 后，ChatGPT 模型会动态加入 `/v1/models`；模型版本以运行时目录为准，不在客户端固定清单。

### GitHub Copilot

| 模型别名 | 实际转发模型 | 说明 |
|----------|--------------|------|
| `copilot` / `auto` / `copilot-auto` | `auto` | GitHub Copilot Chat (自动选择模型) |
| `copilot-opus` / `copilot-sonnet` / `copilot-haiku` | `auto` | 兼容旧别名，统一转发至 `auto` |

### 阿里 MaaS (Qwen 3.8 Max)

- OpenAI 端点: `https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/chat/completions`
- Anthropic 端点: `https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic/v1/messages`

| 模型别名 | 实际模型 | 说明 |
|----------|----------|------|
| `ali-opus` / `qwen3.8-max-preview` | `qwen3.8-max-preview` | 阿里 MaaS 旗舰大模型 |

### 外部提供商

| 模型名 | 提供商 | 说明 |
|--------|--------|------|
| `coding` | 智谱 GLM / 阿里 MaaS | **推荐**，OpenAI 风格，自动 fallback |
| `coding-anthropic` | 智谱 GLM / 阿里 MaaS | Anthropic 风格，自动 fallback |
| `glm-haiku` | 智谱 GLM | 轻量模型 |
| `glm-4.7-flash` | 智谱 GLM | 快速模型 |
| `glm-sonnet` | 智谱 GLM coding plan | 主力模型 |
| `glm-vision` | 智谱 GLM-5V-Turbo | 文本+图片/视频/文件识别 |
| `glm-opus` | 智谱 GLM → 阿里 MaaS → GitHub Copilot | 旗舰模型及知识编译 fallback 链 |
| `ali-opus` | 阿里 MaaS | Qwen 3.8 Max 旗舰模型 |

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
│   │   ├── middleware/      # request ID、日志与指标中间件
│   │   └── requestmeta/     # 请求关联元数据契约
│   ├── .env.example         # 环境变量模板
│   ├── Dockerfile
│   └── README.md            # 完整文档
│
├── scripts/                 # 配套工具脚本
│   └── codex-model          # Codex CLI 模型切换
├── .github/workflows/       # GitHub Actions CI/CD
├── docs/                    # 项目展示页及补充文档
└── README.md                # 本文件
```

## 文档

| 文档 | 说明 |
|------|------|
| [PRD.md](PRD.md) | 产品需求、范围和核心验收 |
| [AGENTS.md](AGENTS.md) | 项目开发准则 |
| [go-gateway/README.md](go-gateway/README.md) | Go 网关完整文档（架构、模型列表、部署） |
| [docs/index.html](docs/index.html) | 项目展示页 |

## 环境变量

| 变量 | 必填 | 说明 |
|------|------|------|
| `LITELLM_MASTER_KEY` | 是 | 网关认证 token |
| `GLM_API_KEY` | 否 | 智谱 API key |
| `ALI_API_KEY`（兼容 `ALIYUN_MAAS_API_KEY` / `DASHSCOPE_API_KEY`） | 否 | 阿里 MaaS (qwen3.8-max-preview) API key |
| `COPILOT_TOKEN` | 否 | GitHub Copilot token（短期有效，约 30 分钟） |
| `COPILOT_GITHUB_TOKEN` | 否 | GitHub OAuth token（用于自动刷新 Copilot token） |
| `HTTP_PROXY` | 否 | HTTP 代理地址（如 `http://127.0.0.1:7890`，启用 ChatGPT Codex） |
| `ADMIN_TOKEN` | 否 | Dashboard/Admin API 独立 token；未设置时回退到主 token |
| `PORT` | 否 | 监听端口（默认 4001） |
| `ARCHIVE_ENABLED` | 否 | 对话归档总开关（默认 false）；启用后供知识库增量导出 |
| `ARCHIVE_MAX_BODY_KB` | 否 | 单条归档 body 安全上限（默认 16384 KB） |
| `ARCHIVE_RETENTION_DAYS` | 否 | 归档保留天数（默认 90） |
| `CIRCUIT_FAILURE_THRESHOLD` | 否 | 连续可重试失败后打开熔断（默认 3） |
| `CIRCUIT_RECOVERY_SECONDS` | 否 | 熔断恢复探测间隔（默认 30） |
| `CIRCUIT_SUCCESS_THRESHOLD` | 否 | 半开状态连续成功次数（默认 1） |

## 资源占用

- 内存：~18 MB
- 启动时间：<1 秒
- Docker 镜像：~50 MB

## 相关项目

- [π-go](https://github.com/hwj123hwj/pi-go) — AI 编程搭档，写代码、搜知识、放音乐

## 展示页

访问 [hwj123hwj.github.io](https://hwj123hwj.github.io) 查看所有项目。

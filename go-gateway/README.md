# go-gateway

轻量级 LLM API 网关，用 Go 编写。支持智谱、小米、美团等多家提供商，内存占用约 18 MB。

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

## 配置 Pi

安装版网关可一键写入 Pi 的自定义模型配置：

```bash
llm-gateway setup pi
```

命令只更新 `~/.pi/agent/models.json` 中的 `llm-gateway` Provider，保留已有配置。默认端点会读取 `~/.llm-gateway/.env` 的 `PORT`，认证密钥不会写入 Pi 配置文件；Pi 使用 `llm-gateway auth print-master-key` 在请求时读取网关主密钥。

```bash
llm-gateway setup pi --dry-run
llm-gateway setup pi --endpoint https://gateway.example.com/v1
```

完成后在 Pi 的 `/model` 中选择 `llm-gateway/coding`。

### 3. 配置 Claude Code

#### Anthropic 兼容客户端

编辑 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4001/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-local-gateway-xxx"
  }
}
```

#### OpenAI 兼容客户端

将 base URL 指向：

- `http://localhost:4001/v1`

聊天接口完整地址为：

- `http://localhost:4001/v1/chat/completions`

---

## API 端点

| 端点 | 方法 | 认证 | 说明 |
|------|------|------|------|
| `/health` | GET | 无需 | 健康检查 |
| `/v1/models` | GET | Bearer | 列出可用模型 |
| `/v1/chat/completions` | POST | Bearer | OpenAI 兼容接口，支持流式 |
| `/v1/messages` | POST | Bearer | Anthropic 兼容接口，支持流式 |
| `/v1/responses` | POST | Bearer | OpenAI Responses API，Codex CLI 专用，支持流式 |
| `/chat/completions` | POST | Bearer | `/v1/chat/completions` 的短路径兼容别名 |
| `/messages` | POST | Bearer | `/v1/messages` 的短路径兼容别名 |
| `/responses` | POST | Bearer | `/v1/responses` 的短路径兼容别名 |

### 日志查看

```bash
# 查看最新 50 行日志
tail -50 /tmp/gw.log

# 实时跟踪日志
# 新开一个终端窗口，运行：
tail -f /tmp/gw.log

# 搜索特定模型的错误
tail -100 /tmp/gw.log | grep -E "glm|copilot|chatgpt"

# 只看错误
tail -100 /tmp/gw.log | grep -E "error|Error|failed"
```

> 日志文件位置：`/tmp/gw.log`
> 日志会被 `pkill` + 重启覆盖，如需保留历史日志请重定向到其他文件。

### 健康检查

```bash
curl http://localhost:4001/health
# {"status":"ok"}
```

### OpenAI 兼容：chat completions（非流式）

```bash
curl -X POST http://localhost:4001/v1/chat/completions \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "coding",
    "messages": [
      {"role": "system", "content": "You are helpful"},
      {"role": "user", "content": "你好"}
    ],
    "max_tokens": 100
  }'
```

### OpenAI 兼容：chat completions（流式）

```bash
curl -N -X POST http://localhost:4001/v1/chat/completions \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "coding",
    "stream": true,
    "messages": [
      {"role": "user", "content": "你好"}
    ]
  }'
```

### Anthropic 兼容：messages（非流式）

```bash
curl -X POST http://localhost:4001/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "coding-anthropic",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

### Anthropic 兼容：messages（流式）

```bash
curl -N -X POST http://localhost:4001/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "coding-anthropic",
    "max_tokens": 100,
    "stream": true,
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

---

## 可用模型

### OpenAI 主链（推荐）

以下模型链优先走 OpenAI 风格上游：

| 模型名 | 上游顺序 | 说明 |
|--------|---------|------|
| `coding` | GLM coding plan → MiMo → LongCat | 日常推荐，自动 fallback |
| `glm-haiku` | GLM coding plan | 轻量快速 |
| `glm-sonnet` | GLM coding plan | 主力模型 |
| `glm-opus` | GLM coding plan | 旗舰模型 |
| `mimo-sonnet` | MiMo OpenAI | 思考模型 |
| `mimo-opus` | MiMo OpenAI | 思考旗舰 |
| `longcat-sonnet` | LongCat OpenAI | 长上下文 |
| `longcat-opus` | LongCat OpenAI | 长上下文旗舰 |

### Anthropic 兼容链

以下模型链用于 `/v1/messages`，走 Anthropic 风格上游：

| 模型名 | 上游顺序 | 说明 |
|--------|---------|------|
| `coding-anthropic` | GLM Anthropic → MiMo Anthropic → LongCat Anthropic | Anthropic 兼容推荐链 |
| `glm-haiku-anthropic` | GLM Anthropic | 轻量快速 |
| `glm-sonnet-anthropic` | GLM Anthropic | 主力模型 |
| `glm-opus-anthropic` | GLM Anthropic | 旗舰模型 |
| `mimo-sonnet-anthropic` | MiMo Anthropic | 思考模型 |
| `mimo-opus-anthropic` | MiMo Anthropic | 思考旗舰 |
| `longcat-sonnet-anthropic` | LongCat Anthropic | 长上下文 |
| `longcat-opus-anthropic` | LongCat Anthropic | 长上下文旗舰 |

### EasyClaw（真实 Claude）

| 模型名 | 实际模型 | 说明 |
|--------|---------|------|
| `easyclaw-sonnet` | claude-sonnet-4-6 | 真实 Claude |
| `easyclaw-opus` | claude-opus-4-6 | 真实 Claude 旗舰 |
| `claude-sonnet-4-6` | claude-sonnet-4-6 | 兼容别名 |

> EasyClaw 使用 OpenAI `/v1/chat/completions` 格式，网关会自动做格式转换。

### ChatGPT Codex（GPT-5.5，OAuth）

使用 ChatGPT Plus/Pro 订阅的 OAuth token 直接调用 OpenAI Codex API，不需要额外 API key。

| 模型名 | 实际模型 | 说明 |
|--------|---------|------|
| `gpt-5.5` | gpt-5.5 | ChatGPT Plus/Pro |
| `gpt-5.5-pro` | gpt-5.5-pro | ChatGPT Pro |
| `gpt-5.4-mini` | gpt-5.4-mini | 轻量快速 |
| `o4-mini` | o4-mini | 推理模型 |

> **前提条件**：
> 1. 在 `.env` 中设置 `HTTP_PROXY=http://127.0.0.1:7890`（国内网络需要代理）
> 2. 已通过 Codex Desktop 登录 ChatGPT，网关会自动读取 `~/.codex/auth.json` 中的 OAuth token
> 3. 请求走 Responses API 透传（`/v1/responses`），不需要格式转换

---

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `LITELLM_MASTER_KEY` | 是 | — | 网关认证 token |
| `GLM_API_KEY` | 否 | — | 智谱 API key |
| `MIMO_API_KEY` | 否 | — | 小米 API key |
| `LONGCAT_API_KEY` | 否 | — | 美团 API key |
| `EASYCLAW_API_KEY` | 否 | — | EasyClaw API key |
| `DEEPV_ENABLED` | 否 | false | 启用 DeepV Server（true/false） |
| `DEEPV_WORK_DIR` | 否 | — | DeepV 工作目录（用于获取 Git 信息） |
| `COPILOT_TOKEN` | 否 | — | GitHub Copilot token（短期有效，约 30 分钟） |
| `COPILOT_GITHUB_TOKEN` | 否 | — | GitHub OAuth token（用于自动刷新 Copilot token） |
| `HTTP_PROXY` | 否 | — | HTTP 代理地址（如 `http://127.0.0.1:7890`），启用 ChatGPT Codex |
| `PORT` | 否 | 4001 | 监听端口 |
| `LOG_LEVEL` | 否 | info | 日志级别 |

未配置 key 的 provider 会被自动跳过，不影响其他 provider 正常工作。

---

## 架构

```text
OpenAI / Anthropic / Codex CLI 客户端
            │
            ▼
┌─────────────────────────────┐
│        Go Gateway           │
│                             │
│  Auth Middleware            │
│  Logging Middleware         │
│                             │
│  Handlers                   │
│  ├── /v1/chat/completions   │
│  ├── /v1/messages           │
│  └── /v1/responses          │
│                             │
│  Router                     │
│  ┌──────────────────────┐   │
│  │ model → provider 映射│   │
│  │ fallback 链管理       │   │
│  └──────────────────────┘   │
│                             │
│  Providers                  │
│  ├── OpenAIProvider         │──▶ GLM coding / MiMo / LongCat / EasyClaw
│  ├── AnthropicProvider      │──▶ GLM / MiMo / LongCat Anthropic
│  ├── CopilotProvider        │──▶ GitHub Copilot (Gemini/GPT)
│  ├── DeepVProvider          │──▶ DeepV (DeepSeek/GLM/Claude/Kimi)
│  └── ChatGPTProvider        │──▶ ChatGPT Codex (OAuth token + proxy)
└─────────────────────────────┘
```

**当前设计**：

- 对外同时提供 OpenAI 与 Anthropic 两套接口
- OpenAI 主链优先直连支持 OpenAI 的上游
- Anthropic 兼容链保留给 `/v1/messages`
- `OpenAIProvider` 会把上游 OpenAI SSE 转为内部可复用流，再由 handler 输出对应协议

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
2. 实现 `Provider` 接口（参考 `anthropic.go` 或 `openai.go`）
3. 如果需要流式，确保支持 `StreamProvider`
4. 在 `internal/config/config.go` 加新字段
5. 在 `main.go` 注册 provider 和 chain
6. 在 `router.go` 的 `mapModelName` 加模型映射

---

## 服务器部署

### 完整地址

- 对外地址: `http://localhost:4001`
- OpenAI 接口: `http://localhost:4001/v1/chat/completions`
- Anthropic 接口: `http://localhost:4001/v1/messages`

### 方式一：GitHub Actions（推荐）

本仓库已包含 GitHub Actions 配置（`.github/workflows/deploy.yml`）：

- 自动运行 `go vet` 和 `go test`
- SSH 到服务器，上传代码构建 Docker 镜像并启动容器

#### 需要在 GitHub 配置的 Secrets

进入仓库 `Settings → Secrets and variables → Actions`，添加以下 Secrets：

| Secret | 说明 |
|--------|------|
| `DEPLOY_HOST` | 服务器 IP，如 `your-server-ip` |
| `DEPLOY_USER` | SSH 用户名，如 `root` |
| `SSH_PRIVATE_KEY` | 服务器 SSH 私钥（完整内容，含换行） |
| `LITELLM_MASTER_KEY` | 网关认证 token |
| `GLM_API_KEY` | 智谱 API key |
| `MIMO_API_KEY` | 小米 API key |
| `LONGCAT_API_KEY` | 美团 API key |
| `EASYCLAW_API_KEY` | EasyClaw API key |

**优势**：
- API keys 通过 Secrets 传入，部署时自动写入 `.env` 并传到服务器
- 不需要在服务器上手动管理 `.env` 文件
- 每次 push 到 `main` 分支自动部署

#### 部署步骤

1. 在 GitHub 仓库 Settings 中配置上述 Secrets
2. 把代码推送到 `main` 分支
3. GitHub Actions 自动触发部署
4. 在 Actions 页面查看部署进度和健康检查结果
5. 部署完成后访问 `http://localhost:4001/health` 验证

### 方式二：手动部署

#### 1. 构建镜像

```bash
cd go-gateway
docker build -t go-llm-gateway:latest .
```

#### 2. 传到服务器

```bash
# SSH 到服务器
ssh user@your-server-ip

# 创建 .env
mkdir -p /opt/go-gateway
cat > /opt/go-gateway/.env << 'EOF'
LITELLM_MASTER_KEY=sk-local-gateway-xxx
GLM_API_KEY=
MIMO_API_KEY=
LONGCAT_API_KEY=
EASYCLAW_API_KEY=
PORT=8080
LOG_LEVEL=info
EOF

# 启动
docker run -d \
  --name go-gateway \
  --restart unless-stopped \
  -p 8080:8080 \
  --env-file /opt/go-gateway/.env \
  go-llm-gateway:latest
```

### 服务器防火墙

确保服务器开放 8080 端口：

```bash
# ufw
ufw allow 8080/tcp

# iptables
iptables -A INPUT -p tcp --dport 8080 -j ACCEPT
```

### 验证

```bash
# 健康检查
curl http://localhost:4001/health
# {"status":"ok"}

# OpenAI 风格
curl -X POST http://localhost:4001/v1/chat/completions \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"coding","messages":[{"role":"user","content":"hi"}]}'

# Anthropic 风格
curl -X POST http://localhost:4001/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"coding-anthropic","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

### 配置 Claude Code（远程）

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4001/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-local-gateway-xxx"
  }
}
```

### 查看日志

```bash
docker logs -f go-gateway
```

### 停止服务

```bash
docker stop go-gateway
docker rm go-gateway
```

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

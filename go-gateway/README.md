# go-gateway

轻量级个人 AI 基础设施网关，用 Go 编写。它统一 OpenAI Chat/Responses 与 Anthropic Messages 入口，按模型能力选择 Provider，并提供 fallback、熔断、指标和管理 API。

Provider、模型别名和路由以本目录的 [`providers.yaml`](providers.yaml) 为准；客户端和文档不维护另一份静态模型清单。运行中的实际目录请以 `GET /v1/models` 为准。

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

网关内置 Dashboard，启动后直接访问 <http://localhost:4001/>。发布二进制会携带 Web 静态资源，不需要单独运行 `npm run dev`；后者只用于 Dashboard 开发调试。

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
| `/readyz` | GET | 无需 | 就绪检查；未加载任何 Provider 时返回 503 |
| `/v1/models` | GET | Bearer | 列出可用模型 |
| `/v1/chat/completions` | POST | Bearer | OpenAI 兼容接口，支持流式 |
| `/v1/messages` | POST | Bearer | Anthropic 兼容接口，支持流式 |
| `/v1/responses` | POST | Bearer | OpenAI Responses API，Codex CLI 专用，支持流式 |
| `/chat/completions` | POST | Bearer | `/v1/chat/completions` 的短路径兼容别名 |
| `/messages` | POST | Bearer | `/v1/messages` 的短路径兼容别名 |
| `/responses` | POST | Bearer | `/v1/responses` 的短路径兼容别名 |

管理面板（使用 `ADMIN_TOKEN`，未配置时回退到 `LITELLM_MASTER_KEY`）：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/admin/providers` | GET | 查看 Provider 运行状态、熔断状态和用量 |
| `/admin/providers/:name` | PATCH | 设置 `{"enabled":true/false}`，运行时启停 Provider |
| `/admin/providers/:name/reset` | POST | 手动重置熔断器 |
| `/admin/providers/:name/health-check` | POST | 执行一次 Provider 健康探测 |
| `/admin/routes` | GET | 查看模型的故障转移顺序 |
| `/admin/routes/:model` | PUT | 用 `{"providers":[...]}` 调整同一链路的优先级 |
| `/admin/models/:model` | PUT | 调整模型 `capabilities` 和 `input_modalities` |
| `/admin/archives` | GET | 分页查询对话归档（`limit`/`offset`） |
| `/admin/archives/export` | GET | 增量导出归档为 JSONL（`since`/`limit`，响应头返回下一游标） |
| `/admin/archives` | DELETE | 按时间清理归档（`before_days` 或 `before`） |
| `/admin/archives/:id` | DELETE | 删除单条归档 |

### 对话归档与增量导出

默认情况下网关只记录轻量指标（状态码、token、延迟），不存任何请求/响应正文。开启归档后会额外保存脱敏后的完整对话，用于知识库增量同步。

**环境变量：**

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARCHIVE_ENABLED` | `false` | 总开关，关闭时归档器为纯 no-op，零开销 |
| `ARCHIVE_MAX_BODY_KB` | `16384` | 单条 request/response body 的安全上限（KB）；超限记录 `truncated=true`，不会写入破损 JSON |
| `ARCHIVE_RETENTION_DAYS` | `90` | 归档保留天数，超期由后台任务自动清理 |

**脱敏规则：** 归档写入前会递归清除 `Authorization`、`Cookie`、`x-api-key`、`api_key`、`token`、`password`、`secret` 等敏感字段的值（替换为 `[REDACTED]`）；图片/音频/文件等多媒体内容只保留 `{type, size, sha256}` 摘要，不存 Base64 原文。流式响应在透传 SSE 的同时 tee 到内部 buffer，流结束后聚合归档；中断时记录原因。

**增量导出示例：**

```bash
# 首次导出
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:4001/admin/archives/export?limit=100" \
  -o batch_1.jsonl

# 响应头 X-Archive-Next-Cursor 给出下一页游标（timestamp,id 格式）
# 带游标续跑，实现断点续传和去重
NEXT=$(curl -sI -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:4001/admin/archives/export?limit=100" \
  | grep -i X-Archive-Next-Cursor | awk '{print $2}' | tr -d '\r')

curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:4001/admin/archives/export?limit=100&since=$NEXT" \
  -o batch_2.jsonl
```

每行 JSON 包含 `schema_version`、`request_id`、`timestamp`、`protocol`、`source`、`conversation_id`、`session_id`、`model`、`provider`、`is_stream`、`status`、`status_code`、`input_tokens`、`output_tokens`、`request_bytes`、`response_bytes`、`truncated`、`request_body`、`response_body`、`error_reason` 等字段。`schema_version` 当前为 `2`。客户端可以通过 `X-AI-Source`、`X-Conversation-ID`、`X-Session-ID` 写入可关联的来源与会话标识；密钥类内容仍会被脱敏。

Provider 熔断默认在连续 3 次可重试上游失败后打开，30 秒后允许一次半开探测；可通过 `CIRCUIT_FAILURE_THRESHOLD`、`CIRCUIT_RECOVERY_SECONDS` 和 `CIRCUIT_SUCCESS_THRESHOLD` 调整。管理接口只返回脱敏的运行状态，API key 始终来自环境变量。

### 模型能力与多模态

`GET /v1/models` 除了标准模型字段，还会返回 `capabilities`、`input_modalities`、`protocol` 和可选的 token 上限。网关会在转发前按这些能力筛选路由：如果请求包含图片而目标模型没有 `vision`，返回明确的 `400`，不会把图片静默降成文本或错误 fallback 到文本模型。

当前配置中：

- `glm-sonnet` 绑定文本模型 `glm-5-turbo`，不是视觉模型；
- `glm-vision` 绑定 `glm-5v-turbo`，用于文本+图片/视频/文件请求；
- 图片请求使用 OpenAI `image_url` content block，网关会保留原始块和 `extra_body`/`thinking` 等扩展字段。

配置新模型时建议显式声明能力：

```yaml
models:
  - id: glm-5v-turbo
    aliases: [glm-vision]
    capabilities: [text, vision, video, file, tool_calling, streaming, reasoning]
    input_modalities: [text, image, video, file]
```

### 日志查看

网关默认将结构化摘要写到 stdout。每个请求都会带 `X-Request-ID`，指标日志中还会记录最终 Provider 和 fallback 尝试；请求正文不会默认写入日志。

```bash
# 运行时保存日志（按需替换为 systemd/Docker 日志收集）
./gateway 2>&1 | tee -a gateway.log

# 按 request ID 检索
grep 'request_id=client-trace-42' gateway.log
```

完整请求/响应归档默认关闭；开启 `ARCHIVE_ENABLED=true` 后会脱敏写入独立的 `conversation_archives` 表，可通过 `/admin/archives` 查询和 `/admin/archives/export` 增量导出，详见下方「对话归档与增量导出」一节。

### 健康检查

```bash
curl http://localhost:4001/health
# {"status":"ok"}

curl http://localhost:4001/readyz
# {"status":"ready"}
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

当前仓库 `providers.yaml` 默认提供以下路由（未配置对应 API key 的 Provider 会自动跳过）：

| 模型名 | 默认上游 | 能力 |
|--------|---------|------|
| `coding` | `glm-glm-5-turbo` → `ali-qwen3.8-max-preview` | 文本、工具调用、推理、流式 |
| `coding-anthropic` | `glm-glm-5-turbo` → `ali-qwen3.8-max-preview` | `/v1/messages` 兼容链 |
| `glm-opus` | GLM `glm-5.2` | 文本、工具调用、推理、流式 |
| `glm-sonnet` | GLM `glm-5-turbo` | 文本、工具调用、推理、流式 |
| `glm-haiku` | GLM `glm-4.7` | 文本、工具调用、推理、流式 |
| `glm-4.7-flash` | GLM `glm-4.7-flash` | 文本、工具调用、流式 |
| `glm-vision` | GLM Vision `glm-5v-turbo` | 文本、图片、视频、文件、工具调用、推理、流式 |
| `ali-opus`, `qwen3.8-max` | 阿里 `qwen3.8-max-preview` | 文本、工具调用、推理、流式 |

配置了可选的 ChatGPT Codex 代理或 GitHub Copilot 后，额外模型会动态加入目录；不要在客户端硬编码版本，直接读取 `/v1/models`。

OpenAI 兼容 Provider 的请求超时默认是 120 秒。需要承载长推理请求时，可在
`providers.yaml` 的 Provider 节点设置 `request_timeout_seconds`；当前 GLM Provider
为知识飞轮的长请求设置了 900 秒。这个值只控制 Provider HTTP 客户端的墙钟超时，
调用方的取消信号仍然优先生效。

---

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `LITELLM_MASTER_KEY` | 是 | — | 网关认证 token |
| `GLM_API_KEY` | 否 | — | 智谱 API key |
| `ALI_API_KEY` | 否 | — | 阿里 MaaS API key（也兼容 `ALIYUN_MAAS_API_KEY`、`DASHSCOPE_API_KEY`） |
| `COPILOT_TOKEN` | 否 | — | GitHub Copilot token（短期有效，约 30 分钟） |
| `COPILOT_GITHUB_TOKEN` | 否 | — | GitHub OAuth token（用于自动刷新 Copilot token） |
| `HTTP_PROXY` | 否 | — | HTTP 代理地址（如 `http://127.0.0.1:7890`），启用 ChatGPT Codex |
| `PORT` | 否 | 4001 | 监听端口 |
| `LOG_LEVEL` | 否 | info | 日志级别 |
| `ADMIN_TOKEN` | 否 | 使用 `LITELLM_MASTER_KEY` | 管理接口独立认证 token |
| `CIRCUIT_FAILURE_THRESHOLD` | 否 | 3 | 连续可重试失败后打开熔断 |
| `CIRCUIT_RECOVERY_SECONDS` | 否 | 30 | 打开后等待半开探测的秒数 |
| `CIRCUIT_SUCCESS_THRESHOLD` | 否 | 1 | 半开状态连续成功后关闭熔断 |
| `ARCHIVE_ENABLED` | 否 | `false` | 对话归档总开关，关闭时零开销 |
| `ARCHIVE_MAX_BODY_KB` | 否 | 16384 | 单条 body 安全上限（KB），超限标记 `truncated=true` |
| `ARCHIVE_RETENTION_DAYS` | 否 | 90 | 归档保留天数 |

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
│  ├── OpenAIProvider         │──▶ providers.yaml 中的 OpenAI 兼容 Provider
│  ├── AnthropicProvider      │──▶ Anthropic 兼容 Provider
│  ├── CopilotProvider        │──▶ GitHub Copilot（可选）
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

1. 在 `internal/provider/` 新建实现（参考 `anthropic.go` 或 `openai.go`）
2. 实现 `Provider` 接口；需要流式时同时实现 `StreamProvider`
3. 在 `providers.yaml` 声明 Provider、模型能力、输入模态和 chain
4. 若需要新的密钥环境变量，同步更新 `.env.example` 和配置加载逻辑
5. 为路由、能力筛选和 fallback 增加测试，并运行 `go vet ./...`、`go test ./...`

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
| `ALI_API_KEY` | 阿里 MaaS API key |

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
ALI_API_KEY=
PORT=4001
LOG_LEVEL=info
ARCHIVE_ENABLED=true
ARCHIVE_MAX_BODY_KB=16384
ARCHIVE_RETENTION_DAYS=90
EOF

# 启动
docker run -d \
  --name go-gateway \
  --restart unless-stopped \
  -p 4001:4001 \
  --env-file /opt/go-gateway/.env \
  go-llm-gateway:latest
```

### 服务器防火墙

确保服务器开放 4001 端口：

```bash
# ufw
ufw allow 4001/tcp

# iptables
iptables -A INPUT -p tcp --dport 4001 -j ACCEPT
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

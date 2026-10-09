# go-gateway

EasyGateway 的网关运行时：轻量级个人 AI 基础设施网关，用 Go 编写。它统一 OpenAI Chat/Responses 与 Anthropic Messages 入口，按模型能力选择 Provider，并提供 fallback、熔断、指标和管理 API。

Provider、模型和路由以本目录的 [`providers.yaml`](providers.yaml) 为准；客户端和文档不维护另一份静态模型清单。运行中的实际目录请以 `GET /v1/models` 为准。

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
go build -o gateway .
./gateway version
./gateway
```

或使用 Makefile：

```bash
make run
```

网关仅提供 HTTP API，不携带或提供浏览器页面。使用 `GET /health` 检查状态，通过 [原生 Go 桌面端](../desktop/README.md) 管理连接、模型、路由、日志和助理。

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
| `/v1/embeddings` | POST | Bearer | OpenAI 兼容嵌入接口，按 model 透传（如 `BAAI/bge-m3`） |
| `/v1/audio/transcriptions` | POST | Bearer | OpenAI 兼容语音转文本（multipart），按 model 透传（如 `TeleAI/TeleSpeech-ASR1.0`） |
| `/embeddings`、`/audio/transcriptions` | POST | Bearer | 对应 `/v1/*` 的短路径兼容别名 |

透传端点不做格式转换：请求/响应原样转发给模型所属 Provider；模型须在 `providers.yaml` 注册，Provider 的 `url` 配上游源站，网关在其后拼接端点路径。

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
| `/admin/pi` | GET | 查看 Pi 模型清单同步状态（期望清单、当前清单、差异） |
| `/admin/pi/sync` | POST | 把精选模型清单同步到 `~/.pi/agent/models.json`（写入前自动备份），等价于 `llm-gateway setup pi`，也可在控制面板「设置 → Pi 集成」一键操作 |
| `/admin/archives` | GET | 分页查询对话归档（`limit`/`offset`） |
| `/admin/archives/export` | GET | 增量导出归档为 JSONL（`since`/`limit`，响应头返回下一游标） |
| `/admin/archives` | DELETE | 按时间清理归档（`before_days` 或 `before`） |
| `/admin/archives/:id` | DELETE | 删除单条归档 |
| `/admin/skills` | GET | 技能面板状态：技能目录、启用清单、各目标目录链接是否同步（未设置 `SKILLS_REPO_PATH` 时返回 `configured:false`） |
| `/admin/skills/:id` | GET | 单个技能详情（registry 条目 + SKILL.md 原文） |
| `/admin/skills/config` | PUT | 写入启用清单 `{"targets":[...],"enabled":[...]}`（写入前自动备份） |
| `/admin/skills/sync` | POST | 按清单落盘：为启用技能建符号链接、清理指向技能仓库的旧链接 |

### 技能面板（custom-skills 技能市场）

桌面端的 Skills 页在设置 `SKILLS_REPO_PATH` 指向 custom-skills 仓库根目录后可用。仓库内的 `registry/skills.json` 是技能目录（`generate:registry` 生成），`skills.enabled.json` 是启用清单（声明哪些技能装到哪些目录）。同步用**符号链接**把仓库的 `skills/<id>` 铺进目标目录：仓库更新即时生效，重装系统后一条同步命令即可恢复；清理时只移除指向技能仓库的链接，目标目录里的其他文件一律不动。

目标目录属于**运行网关的主机**，使用绝对路径或 `~/`（网关进程用户的 home）；不支持相对目标路径。技能 ID 必须是 registry 内不含路径分隔符的目录名。保存与同步均校验清单，保存采用临时文件替换，并串行处理管理端的保存/同步请求。`targets` 和 `enabled` 都必须显式传入数组，全部停用用 `enabled: []`。

同步结果中的 `linked/current/removed/skipped/errors` 始终为数组；有同名文件冲突时跳过并保持 `in_sync: false`，其他错误显示在面板。同步只检查当前清单中的目标目录：如需移除旧目标内的技能，先停用并同步旧目标，再修改目标列表。

页面支持 **全局技能 / 项目技能** 两种范围：
- 全局范围继续使用技能仓库的 `skills.enabled.json` 与配置的目标目录。
- 项目范围输入现有项目的绝对路径，每个项目独立保存 `.agents/skills.enabled.json`，仅同步到该项目的 `.agents/skills`；不会改变全局启用清单。清单内保存相对目标，便于项目移动。
- 「本地已有」只读展示未由技能库管理的技能；项目范围检查 `.agents/skills`、`.claude/skills` 和 `.codex/skills`。同名自定义文件和外部链接不会被覆盖；全局与项目技能的实际加载及优先级由客户端决定。
- `/admin/skills`、`/admin/skills/config`、`/admin/skills/sync` 可携带 `?project=<URL 编码的项目绝对路径>`，省略时保持全局行为。项目目录需存在；项目 `.agents`、安装目录和清单不接受符号链接，以免跨范围写入。
- 选择技能会先形成草稿。「仅保存」保存清单，「保存并同步」才落盘。同步结果区分新增、清理、同名跳过及错误，空结果返回数组。

`/admin/stats`、`/admin/dashboard`、`/admin/models` 和 `/admin/logs` 会返回聚合指标或请求日志。缓存统计包含 `cache_read_input_tokens`、`cache_creation_input_tokens`、`cache_input_tokens`、`cache_usage_requests` 和 `cache_hit_rate`；命中率按缓存读取 token ÷ 已报告的总输入 token 加权计算。上游没有返回缓存 usage 的请求不计入命中率样本，数据不足时 `cache_hit_rate` 为 `null`。

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

### Provider 探测

`POST /admin/providers/:name/health-check` 会**真实发一次最小请求到上游**，用来验证「鉴权 + 模型可用」，而不是只看本地配置。结论分四类：

| 结论 | 含义 | 触发条件 |
|------|------|---------|
| `online` | 上游可用 | 上游返回 2xx |
| `degraded` | 上游可达但当前受限，通常可自愈 | 429、5xx、网络错误 |
| `offline` | 需要改配置才能恢复 | 401/402/403/404（凭据失效、无模型权限、欠费、模型不存在） |
| `unknown` | 无法得出结论 | provider 未实现探测能力 |

响应里带 `probe_status`、`detail`、`status_code`、`latency_ms`，并同时返回该 provider 的完整运行状态。只有 `online`/`degraded`/`offline` 会写入熔断器；`unknown` 不写结果，避免一次「测不了」被当成成功而清掉真实的连续失败计数。

管理面板的「在线」状态以**最近一次探测结论**为准，优先于「历史上请求成功过」——否则一个已被上游限流的 provider 会因为早先成功过而长期显示在线。从未探测过的 provider 显示「未知」，面板会注明状态来自请求历史。

`Provider.IsHealthy` 只反映本地配置（凭据是否存在），不再作为上游可用性的判断依据。

### 模型能力与多模态

`GET /v1/models` 除了标准模型字段，还会返回 `capabilities`、`input_modalities`、`protocol` 和可选的 token 上限。网关会在转发前按这些能力筛选路由：如果请求包含图片而目标模型没有 `vision`，返回明确的 `400`，不会把图片静默降成文本或错误 fallback 到文本模型。

当前配置中：

- 智谱只提供 `glm-5.3` 与 `glm-5.3-flash` 两个模型，其余型号（含视觉专用的 `glm-5v-turbo`）已从网关删除；
- `glm-5.3` 是纯文本模型，不支持图片（上游会以 `1210 messages.content.type 参数非法` 拒绝）；
- `glm-5.3-flash` 兼有图片能力，`coding` 链上的图片请求会跳过 `glm-5.3` 落到它；
- Antigravity（Google 账号 OAuth）经本机 CLIProxyAPI 反代为 OpenAI 兼容上游，
  `gemini-3.8-flash-high`（当前 Gemini 系列主力）实测支持图片输入；OAuth 凭据与 token 续期都在 CLIProxyAPI 侧维护；
- 图片请求使用 OpenAI `image_url` content block，网关会保留原始块和 `extra_body`/`thinking` 等扩展字段。

配置新模型时建议显式声明能力：

```yaml
models:
  - id: glm-5.3-flash
    capabilities: [text, vision, tool_calling, streaming, reasoning]
    input_modalities: [text, image]
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
    "model": "coding",
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
    "model": "coding",
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
| `coding` | `glm-5.3` → `glm-5.3-flash` → `gemini-3.8-flash-high` → `deepv-glm-5.3-flash` → `deepseek-flash` | 文本、工具调用、推理、流式；图片请求自动跳过 `glm-5.3` |
| `glm-5.3` | 智谱 `glm-5.3` | 文本、工具调用、推理、流式（不支持图片） |
| `glm-5.3-flash` | 智谱 `glm-5.3-flash` | 文本、图片、工具调用、推理、流式 |
| `gemini-3.8-flash-high` | Antigravity `gemini-3.8-flash-high`（本机 CLIProxyAPI 反代，`-high` 为思考档后缀） | 文本、图片、工具调用、推理、流式 |

**命名规则：模型名就是上游模型 ID，不设别名。** 客户端要调哪个模型就写哪个名字，
网关不再维护 `glm-opus` / `glm-haiku` / `ali-opus` 这类第二套代号。
provider 实例名同样用上游模型 ID；只有不同供应商提供同名模型时（智谱与 DeepV
都有 `glm-5.3-flash`）才加供应商前缀区分。

**`coding` 是唯一的对外入口，也是唯一的降级链。** 链上按能力从强到弱排列，
任一档失败都会自动降级到下一档，客户端始终只调用 `coding` 一个名字：

- 上游限流（429）、5xx、网络错误 → 换下一档重试
- 账号无该模型权限（403）、凭据失效（401）、欠费（402）、模型不存在（404）→ 换下一档重试
- 请求本身不合法（400/422）→ **不**降级，直接返回（换个 provider 也是同样的错误）
- 请求带图片时，不具备视觉能力的档位会被自动跳过

启用 `DEEPV_ENABLED=true`（EasyCode/DeepVCode 聚合服务，自动读本地 JWT 登录态）后，额外提供：

| 模型名 | 上游绑定 | 能力 |
|--------|---------|------|
| `deepseek-flash`（兼容名 `deepseek-v4.1-flash`） | DeepV `deepseek-flash` | 文本、图片、工具调用、推理、流式 |
| `deepv-glm-5.3-flash` | DeepV `glm-5.3-flash` | 文本、图片、工具调用、推理、流式 |

DeepV 工具调用适配会合并 JSON 字符串参数分片及同一调用 ID 的重复帧，完整参数收齐后再交给客户端执行；历史调用即使没有参数也保留 `args: {}`。不完整或非对象参数会明确报错，避免静默丢失后继续执行工具。

DeepV 上游按单请求 token 总量（输入 + `max_output_tokens`）不超过 200000 校验，两个模型的目录条目声明 `max_input_tokens: 160000`、`max_output_tokens: 32000`。超过该限制的请求会被上游以配额错误拒绝，网关识别后转换为 400 并附处置说明，避免客户端把参数问题当成欠费（402 Payment Required）。

配置了 ChatGPT Codex OAuth 凭证或 GitHub Copilot 后，额外模型会动态加入目录；ChatGPT 的代理是可选的。不要在客户端硬编码版本，直接读取 `/v1/models`。

OpenAI 兼容 Provider 的请求超时默认是 120 秒。需要承载长推理请求时，可在
`providers.yaml` 的 Provider 节点设置 `request_timeout_seconds`；当前 GLM 与
Antigravity Provider 为知识飞轮的长请求设置了 900 秒。这个值只控制 Provider HTTP 客户端的墙钟超时，
调用方的取消信号仍然优先生效。

---

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `LITELLM_MASTER_KEY` | 是 | — | 网关认证 token |
| `GLM_API_KEY` | 否 | — | 智谱 API key |
| `CLIPROXY_API_KEY` | 否 | — | 本机 CLIProxyAPI（Antigravity 反代）的静态 api-key |
| `COPILOT_TOKEN` | 否 | — | GitHub Copilot token（短期有效，约 30 分钟） |
| `COPILOT_GITHUB_TOKEN` | 否 | — | GitHub OAuth token（用于自动刷新 Copilot token） |
| `DEEPV_ENABLED` | 否 | `false` | 启用 DeepV Server（EasyCode/DeepVCode，deepseek / glm-5.3-flash 系列） |
| `DEEPV_WORK_DIR` | 否 | 启动目录 | DeepV 请求附带的 Git 信息头来源目录 |
| `HTTP_PROXY` | 否 | — | ChatGPT Codex 的可选 HTTP 代理地址（如 `http://127.0.0.1:7890`） |
| `CHATGPT_AUTH_FILE` | 否 | 自动查找 | ChatGPT/Pi OAuth `auth.json` 路径 |
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
│  └── ChatGPTProvider        │──▶ ChatGPT Codex (OAuth token; proxy optional)
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
   DeepV 使用专用协议，模型及独立路由在 `main.go` 的 `setupDeepVProviders` 中注册；ChatGPT 订阅模型还需同步 `internal/provider/chatgpt.go` 的备用目录。新增后需重新构建并重启对应运行实例才能生效。
4. 若需要新的密钥环境变量，同步更新 `.env.example` 和配置加载逻辑
5. 为路由、能力筛选和 fallback 增加测试，并运行 `go vet ./...`、`go test ./...`

---

## 服务器部署

### 完整地址

- 对外地址: `http://localhost:4001`
- OpenAI 接口: `http://localhost:4001/v1/chat/completions`
- Anthropic 接口: `http://localhost:4001/v1/messages`

### 方式一：迷你主机自动同步（推荐）

.github/workflows/ci.yml 在每个 PR 上运行 Go 检查、迷你主机更新器回归、Windows 安装器回归和 Linux x86_64 构建；合并到 main 后，只有这些检查全通过，GitHub Actions 才会发布迷你主机使用的构建产物。PR 构建不会发布给更新器。

迷你主机上的用户级 systemd timer 每 10 分钟检查一次 GitHub。主机只会部署与当前 main 完全一致、CI 成功且摘要校验通过的构建产物；安装后会检查网关健康状态，并触发现有 systemd 服务重启。如果更新后健康检查失败，会恢复上一份二进制。构建失败、检查进行中或 main 已继续前进时，现有网关不变。

该流程只需要迷你主机访问 GitHub 的出站 HTTPS，不配置 GitHub Secrets、部署私钥、入站端口或仓库 Runner。首次安装前，在 GitHub 创建只对本仓库开放、仅有 Actions Read 权限的 fine-grained token，并在主机终端隐藏输入：

```bash
bash -c 'install -d -m 700 ~/.config/litellm-gateway; read -r -s -p "GitHub Actions read token: " EA_GITHUB_TOKEN; printf "\\n"; printf "%s" "$EA_GITHUB_TOKEN" > ~/.config/litellm-gateway/github-token; unset EA_GITHUB_TOKEN; chmod 600 ~/.config/litellm-gateway/github-token'
```

令牌只用于读取 GitHub Actions API 元数据和成功 CI 生成的构建产物，保存在主机的独立权限文件中，不会进入服务环境变量、进程参数或日志。主机保留自己的 .env 和 provider 凭据。随后在主机上运行一次安装命令（此项目默认使用 Linux 用户 q 和 llm-gateway.service）：

    set -o pipefail
    curl --fail --silent --show-error https://api.github.com/repos/hwj123hwj/litellm-gateway/contents/scripts/install-mini-gateway-updater.sh | python3 -c 'import base64,json,sys; print(base64.b64decode("".join(json.load(sys.stdin)["content"].split())).decode(), end="")' | bash

如果从 Mac 执行，可通过 SSH 完成这次凭据录入和安装；后续检查、下载和重启均由主机本地 timer 完成，无需逐次 SSH。安装脚本不使用 sudo；配置文件位于 ~/.config/litellm-gateway/update.env。

查看同步状态和最近日志：

    systemctl --user status gateway-auto-update.timer
    journalctl --user -u gateway-auto-update.service -n 30 --no-pager

只有网关进程健康时才会更新；失败的版本会记在 ~/.local/state/litellm-gateway-update/failed-revision，排除问题后删除该文件即可重试。

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
  -d '{"model":"coding","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
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

客户端集成面板支持分别勾选 EasyAgent、ZCode 和 Harness 要同步的聊天模型。默认勾选客户端当前已有的模型，新增模型不会自动加入；首次配置默认全选。同步只替换该客户端的网关模型清单，并在写入前备份。取消勾选的模型会从清单移除；至少选择一个模型才能同步。

降级链遇到 Gemini 的 `400 User location is not supported for the API use` 时，会继续尝试下一供应商；其他 HTTP 400 参数错误与 HTTP 422 仍终止降级。

DeepV 的 `claude-haiku-5-5` 不接受 `temperature`；网关转换该模型请求时会省略此字段，其他模型保持原有转发行为。

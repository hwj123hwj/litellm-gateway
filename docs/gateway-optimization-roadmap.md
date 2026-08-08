# Gateway 后续问题与优化路线图

> 创建时间：2026-08-07
>
> 范围：`go-gateway` 以及 HwjCode 的本地网关集成
>
> 更新时间：2026-08-08
>
> 状态：能力协商、图片路由、流式生命周期、错误码和 Responses token 映射已完成；其余条目用于后续迭代

## 一、当前基线

- 网关当前监听 `http://localhost:4001`，`GET /health` 已验证返回 200。
- `go test ./...` 已通过。
- `glm-sonnet` 当前映射到文本模型 `glm-5-turbo`，只声明文本能力。
- `glm-vision` 当前映射到视觉模型 `glm-5v-turbo`，声明支持图片、视频和文件。
- `glm-vision` 的真实识图请求已经正确路由到智谱视觉接口；当时上游返回余额/资源包不足（429），不是网关格式转换失败。
- SQLite 当前保存请求元数据（模型、Provider、状态码、耗时、Token 等），默认清理 30 天前的数据。
- 完整请求体目前还会写入普通 stdout 日志，后续应改造成可配置的结构化记录。

## 二、已经确定的设计决策

### 1. 不删除完整对话记录

这是个人部署的网关，保留自己的请求和响应用于使用分析是合理需求。后续不删除完整记录，而是把它从普通运行日志中分离出来，成为独立的“个人 AI 使用审计”能力。

### 2. 网关继续作为统一 Router

HwjCode、飞书入口以及未来其他 AI 应用都可以走同一个网关，统一获得：

- 模型和 Provider 路由
- fallback 和错误统计
- Token、延迟、成功率统计
- 对话归档和使用分析

### 3. `glm-sonnet` 不改成视觉别名

`glm-sonnet` 保持文本模型语义，视觉请求使用 `glm-vision`。这样可以避免模型名称、升级策略和实际能力再次混淆。

## 三、待处理事项

### 已完成：能力协商和图片模型自动选择

网关与 HwjCode 已完成以下改造：

- `/v1/models` 返回 `capabilities`、`input_modalities`、`protocol` 和可选 token 上限。
- 网关在转发前按请求能力筛选 Provider，图片发给文本模型时返回 400，不再静默丢图或错误 fallback。
- OpenAI Chat Completions、Responses 与 Anthropic 入口的图片块会保留并转换到 OpenAI 视觉上游。
- HwjCode 读取模型能力元数据，图片、视频、文件场景按能力选择自定义模型；`ImageReaderTool` 不再依赖 Gemini 名称。
- 没有兼容模型时，HwjCode 会立即提示刷新 `/v1/models` 或配置 `glm-vision`。

2026-08-08 实机验收：

- `glm-sonnet` 文本请求返回 200；`max_tokens=1024` 时内容正常。
- 图片请求指定 `glm-sonnet` 时返回 400，且未调用文本上游。
- 图片请求指定 `glm-vision` 时已正确到达 `glm-5v-turbo`；上游返回 429（余额/资源包不足），说明当前剩余阻塞是账号资源，不是网关格式或路由。
- HwjCode 的 inline image 能力识别、自动选模与无兼容模型提示均有回归测试。

相关代码：

- `go-gateway/providers.yaml`
- `go-gateway/internal/provider/capabilities.go`
- HwjCode `packages/core/src/core/gatewayContentGenerator.ts`
- HwjCode `packages/core/src/tools/image-reader.ts`
- HwjCode `packages/core/src/core/sceneManager.ts`

### 已完成：流式 fallback、错误码和 token 上限（2026-08-08）

- Provider 在首个 SSE 字节写出前失败时，不会提前提交 200；网关可以继续 fallback 或返回上游状态码。
- 首个 SSE 字节写出后不再切换 Provider，而是发送规范的流式错误事件并结束当前响应。
- Provider HTTP 错误统一保留状态码、`Retry-After` 和 request id；401/403/400 等客户端错误不再被错误包装成 503，也不会继续 fallback。
- `Responses API` 的 `max_output_tokens` 现在会映射到内部 `max_tokens`，不再被静默丢弃。
- 已增加对应的 handler/provider 回归测试，并通过 `go test ./...`、`go vet ./...` 和 `go test -race ./...`。

### P0：结构化、可配置的完整日志

**问题**

当前完整请求体直接写入 stdout，可能包含系统提示词、私聊内容、工具参数和图片 Base64；它既不便于按会话分析，也可能造成日志无限增长。

**建议设计**

增加配置项，例如：

```env
# metadata：只记录指标；full：记录请求和响应；off：关闭请求归档
REQUEST_LOG_MODE=metadata
REQUEST_LOG_RETENTION_DAYS=90
REQUEST_LOG_MAX_BODY_BYTES=10485760
```

`full` 模式单独写入 SQLite 或 JSONL 归档，字段至少包括：

- `request_id`
- `source_app`
- `session_id` / `conversation_id`
- 请求和响应时间
- 模型、Provider、状态码、耗时、Token
- 请求消息和响应内容
- 错误摘要及重试次数

安全和容量约束：

- 不保存 Authorization、Cookie 等请求头。
- 图片正文可保存为内容哈希加文件引用，避免重复 Base64。
- 归档文件使用当前用户可读权限，并支持保留期和大小上限。
- stdout 只保留摘要；完整内容仅在 `full` 模式下写入归档。

相关代码：

- `go-gateway/internal/handlers/chat_completions.go`
- `go-gateway/internal/handlers/messages.go`
- `go-gateway/internal/metrics/collector.go`
- `go-gateway/internal/storage/sqlite.go`

### 已完成：错误码、重试和 fallback 语义

当前实现已按以下规则区分错误：

| 错误类型 | 建议行为 |
| --- | --- |
| 请求参数错误 | 400，不 fallback |
| 未知模型 | 404，不 fallback |
| 不支持的能力 | 400，不 fallback |
| 401 / 403 | 直接返回，提示凭证或权限问题 |
| 429 / 余额不足 | 保留限流/配额语义，可按策略重试 |
| 网络错误、部分 5xx | 允许 fallback |

同时保留上游 request id 和 `Retry-After`，但不要把完整上游响应体原样写入普通日志。

### 已完成：流式 fallback 的响应生命周期

流式处理在第一次 Provider 调用成功前不应立即写出 200 响应头。需要区分：

- 尚未发送任何 SSE 数据：可以 fallback 或返回正确状态码。
- 已经发送 SSE 数据：不能再切换 Provider，只能发送规范的流式错误事件并结束。

### P1：认证、文档和配置统一

网关实际要求 `/v1` 使用 Bearer Token，但部分 HwjCode 注释仍描述为“不校验认证”。建议保留认证机制，并统一：

- 本地客户端的 `apiKey` 使用网关 master key。
- README、注释和配置向导明确说明认证要求。
- `/health` 保持公开，模型和管理接口继续要求认证。

### P1：固定二进制和进程管理

当前主要通过 `go run .` 运行，重启时依赖外层进程自动拉起。后续建议：

- 构建固定路径的二进制。
- 使用 macOS launchd 或明确的启动脚本管理生命周期。
- 统一日志文件、退出码和自动重启策略。
- 启动后自动执行 `/health` 和 `/v1/models` 检查。

### P2：协议和模型兼容性补强

- 增加 OpenAI Chat Completions、Anthropic Messages、Responses 三条入口的集成测试。
- 增加文本、图片、视频、文件、工具调用、流式响应测试。
- 检查 OpenAI `reasoning_content` 是否需要映射到内部响应，避免推理信息被静默丢弃。
- 对 reasoning 模型的极小 `max_tokens` 给出清晰提示；不建议网关偷偷改写用户传入值。
- `/v1/models` 的能力字段在客户端模型选择界面中展示和过滤。

## 四、建议实施顺序

1. 能力协商和 `glm-vision` 自动选择。
2. 结构化完整日志和个人使用分析存储。
3. 错误码、重试与流式 fallback（已完成，后续继续补充跨 Provider 集成测试）。
4. 认证文档和固定二进制运行方式。
5. 扩充跨协议集成测试及推理字段兼容性。

## 五、暂不做的事情

- 不把 `glm-sonnet` 改成视觉模型别名。
- 不删除现有 SQLite 指标统计。
- 不把完整对话默认上传到外部服务。
- 不为了日志分析重写 Git 历史或引入重量级数据库。

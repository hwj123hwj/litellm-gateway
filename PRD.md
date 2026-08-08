# PRD：个人 AI 基础设施网关

> 版本：2.1
> 状态：持续演进
> 最后更新：2026-08-08

## 1. 文档目的

本文只定义产品需求和范围，不替代技术设计、接口实现文档或部署手册。

## 2. 背景

目前同时使用 HwjCode、Claude Code、Codex CLI、Pi、飞书内置能力等多个 AI 应用，也使用智谱、阿里以及其他模型 Provider。

如果每个应用都直接连接 Provider，会产生以下问题：

- Provider 地址、Key、代理和模型配置重复；
- 不同应用的协议转换、模型别名和 fallback 行为不一致；
- 文本模型和视觉模型容易选错；
- Provider 故障无法统一恢复；
- 请求、错误和使用记录分散；
- 有价值的对话无法统一沉淀为个人知识。

因此，网关需要从“API 转发器”升级为所有 AI 应用共用的个人基础设施。

## 3. 产品定位

LLM Gateway 负责四件事：

1. 统一接入：为所有 AI 应用提供统一地址、认证和模型目录；
2. 统一调度：根据模型能力和运行状态选择 Provider；
3. 统一管理：提供健康、路由、日志和配置控制；
4. 统一沉淀：在用户开启后归档对话，并向知识库提供导出。

网关是个人部署、单用户优先的基础设施，不是面向公众的多租户 SaaS。

## 4. 目标与非目标

### 4.1 目标

- 一个端点接入多个 AI 应用和多个 Provider；
- 兼容 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages；
- 支持文本、多模态、工具调用、推理和流式请求；
- 支持模型能力路由、fallback、健康检查和熔断；
- 提供 Web Dashboard 和唯一的 React Native Android App；
- 记录统一指标，并支持个人场景下的完整对话归档；
- 将网关对话导出到 agent-lessons、hwj-wiki 等知识处理工具；
- 保持单二进制、跨平台、易安装和易升级。

### 4.2 非目标

- 不训练或托管模型；
- 不向上层应用暴露 Provider Key；
- 不在网关内实现完整 Wiki、向量数据库或知识编译器；
- 不维护桌面客户端、旧 Capacitor Android 客户端或静态 UI 原型；
- 当前不做多租户、计费和复杂组织权限。

## 5. 典型使用场景

### 场景 A：AI 应用统一调用

HwjCode、Claude Code、Codex CLI、Pi 和后续应用只配置网关地址与 Key，不再分别配置 Provider。

### 场景 B：模型能力选择

文本请求使用文本模型；图片、视频或文件请求自动选择声明了对应能力的模型，例如 glm-vision，而不是错误地落到 glm-sonnet。

### 场景 C：Provider 故障恢复

主 Provider 超时、限流或不可用时，网关按配置切换备用 Provider；连续失败后熔断，恢复后自动探测。

### 场景 D：个人对话沉淀

用户开启归档后，网关保存经过脱敏处理的请求和响应，随后导出给知识处理工具，形成知识卡、项目日志和可检索内容。

### 场景 E：运行管理

用户通过 Web 或 Android 查看 Provider、模型、路由、请求日志和健康状态，并执行启停、健康探测和熔断重置。

## 6. 核心需求

### R1：统一 API 入口

- 提供一个稳定的网关地址和 Bearer 认证；
- 支持 OpenAI Chat Completions；
- 支持 OpenAI Responses；
- 支持 Anthropic Messages；
- 支持流式和非流式请求；
- 生成 request ID，便于排查和关联日志；
- 上层应用不需要知道 Provider 的真实地址和 Key。

### R2：模型目录与能力路由

网关通过 /v1/models 暴露当前可用模型和能力，至少包括：

- 模型 ID 和别名；
- Provider 和实际绑定模型；
- text、vision、video、file、tool_calling、streaming、reasoning 等能力；
- 输入模态和协议类型。

路由要求：

- 先判断请求需要的能力，再选择 Provider；
- 图片请求不能静默发送给纯文本模型；
- 客户端不得把某个 Provider 的模型硬编码为所有场景的默认值；
- Provider 配置和 /v1/models 是模型信息的事实来源。

### R3：Provider 调度与可靠性

- 支持 Provider 优先级和模型链；
- 支持可重试错误的自动 fallback；
- 支持健康检查；
- 支持连续失败熔断和恢复探测；
- 支持运行时启用、禁用和重置 Provider；
- 记录最终 Provider 以及各次 Provider 尝试；
- 不把一次用户请求的多次 fallback 误算成多次逻辑请求。

### R4：管理控制面

Admin API 和管理端至少支持：

- 查看 Provider 状态、健康结果和熔断状态；
- 查看模型和路由；
- 修改路由优先级；
- 启停 Provider；
- 手动执行健康检查；
- 重置熔断器；
- 查看 Dashboard、请求日志和聚合统计；
- 返回脱敏配置摘要，不返回明文 Key。

### R5：统一指标与日志

网关至少记录：

- 时间、请求 ID、协议和路径；
- 请求模型、最终 Provider；
- 状态码、错误摘要和延迟；
- 输入/输出 Token；
- 是否流式；
- fallback 和 Provider 尝试结果。

指标日志与完整正文分开保存，不能依赖 stdout 作为唯一历史记录，也不能让指标表被大请求正文拖垮。

### R6：可控的完整对话归档

个人部署可以开启完整请求/响应归档，但必须具备：

- Chat、Messages、Responses 三条路径一致处理；
- 非流式响应完整保存；
- 流式响应聚合后保存终态；
- passthrough 路径也能被关联；
- Authorization、Cookie、Token、密码和 API Key 永不落库；
- 支持大小限制、保留天数、清理和导出；
- 归档失败不影响主请求返回；
- 归档数据只对经过认证的管理端可见。

多媒体内容默认保存类型、大小、哈希和描述，原始大文件或 Base64 只有在用户明确开启时才保留。

### R7：知识库导出

网关不负责知识编译，只负责提供稳定导出：

- 支持按时间和游标增量导出；
- 导出内容默认脱敏；
- 导出格式包含 schema 版本；
- 重复导出可以去重；
- 导出记录包含来源、请求 ID、模型、Provider、消息、工具调用、状态和使用信息。

职责分工：

- 网关：采集、保护、归档、导出；
- agent-lessons：多源采集、清洗、编译和分类；
- hwj-wiki personal：个人知识库和检索；
- pi-go .llm-wiki：代码事实和项目技术知识。

### R8：管理端与部署

正式管理端：

- web/：React + Vite Web Dashboard；
- mobile-app/：React Native + Expo Android App。

部署要求：

- Go 单二进制运行；
- 提供 macOS、Linux、Windows 安装脚本；
- 支持本地、服务器和个人设备部署；
- 配置与密钥分离；
- 支持健康检查、自动发布和自动部署；
- 升级不覆盖用户配置和数据。

## 7. 当前实现状态

| 能力 | 状态 | 说明 |
|---|---|---|
| 三种协议入口 | ✅ 已实现 | Chat Completions、Responses、Messages |
| 流式请求 | ✅ 已实现 | 三种协议均有实现 |
| Provider 配置与模型链 | ✅ 已实现 | providers.yaml + 环境变量 |
| fallback、健康和熔断 | ✅ 已实现 | 支持运行时控制 |
| 能力感知模型目录 | ✅ 已实现 | /v1/models 返回能力和输入模态 |
| 多模态路由 | ✅ 已实现 | 文本模型与 glm-vision 区分 |
| SQLite 指标 | ✅ 已实现 | request_logs、daily_stats |
| 请求关联与 Provider 尝试记录 | ✅ 已实现 | `X-Request-ID`、最终 Provider、每次 fallback 尝试均进入轻量指标日志 |
| Admin API | ✅ 已实现 | Provider、模型、路由、日志和统计 |
| Web Dashboard | ✅ 已实现 | web/ |
| Android App | ✅ 已实现 | mobile-app/ |
| 跨平台安装和发布 | ✅ 已实现 | GitHub Actions |
| 完整对话归档 | ⚠️ 未完成 | 当前没有独立归档模型 |
| 流式终态归档 | ❌ 未实现 | 需要聚合 SSE |
| 网关知识导出 | ❌ 未实现 | 需要版本化导出契约 |
| 多源统一检索 | 🟡 规划中 | 由知识库项目负责 |

## 8. 验收标准

### API 和路由

- 三种协议均可完成流式和非流式请求；
- 工具调用、推理和多模态内容不会被静默丢失；
- 图片请求不会错误路由到只支持文本的模型；
- /v1/models 与实际可用配置一致；
- Provider 失败时 fallback、熔断和恢复行为符合配置。

### 管理和安全

- 未认证访问 Admin API 返回 401；
- Web 与 Android 使用同一套 Admin API；
- Provider Key 不出现在 API 响应、日志和发布产物中；
- Provider 启停、路由调整、健康检查和熔断重置可生效；
- 本地和服务器部署均能通过健康检查。

### 归档和知识

- 三种协议都能产生统一格式的归档记录；
- 流式请求能记录完整终态或明确中断原因；
- 归档支持脱敏、清理和大小限制；
- 导出支持增量、断点续跑和去重；
- 导出结果可以被知识处理工具消费。

## 9. 交付顺序

### P0：基础设施稳定

- [x] 补齐 request ID、最终 Provider 和 Provider attempt 记录；
- [x] 让轻量指标与请求正文分离，默认不把正文写入 stdout；
- [x] 让 README、PRD、providers.yaml 和 /v1/models 保持一致；
- [ ] 继续统一错误、认证和 Dashboard 行为。

### P1：对话归档

- 独立归档数据模型；
- 覆盖三种协议、流式和 passthrough；
- 完成脱敏、大小限制、保留和清理；
- 增加归档相关回归测试。

### P1：知识出口

- 实现 JSONL/CLI 增量导出；
- 接入 agent-lessons；
- 导入 hwj-wiki personal。

### P2：统一检索

- 明确 agent-lessons、hwj-wiki 和 pi-go 的职责；
- 提供跨来源的关键词、标签和向量检索。

## 10. 关键决策

1. 网关是个人 AI 基础设施，不只是 API 代理。
2. 数据平面、控制平面、指标日志和知识导出分层。
3. 完整对话归档是个人部署的可控能力，不与轻量指标表混用。
4. Provider 和模型以配置及 /v1/models 为准，客户端不硬编码 Provider。
5. web/ 和 mobile-app/ 是正式管理端，不维护 desktop/、旧 Android 工程或静态 mockup。
6. 先完成个人单机闭环，再考虑多用户、分布式和计费。

# PRD：个人 AI 基础设施网关（LLM Gateway）

> 版本：2.0
> 状态：持续演进
> 最后更新：2026-08-08
> 产品定位：Local-first 的个人 AI 基础设施，而不是单纯的 API 转发器

## 0. 一句话定义

LLM Gateway 是所有 AI 应用访问模型提供商的统一基础设施层：统一入口、协议兼容、模型能力路由、Provider 编排、故障转移、运行控制、可观测性、对话归档和知识库导出都由它提供。

它的价值不只在于“把请求转发给模型”，而在于让上层应用不再感知 Provider 差异，让每一次 AI 使用都可控、可观测、可恢复，并能够沉淀为个人知识资产。

## 1. 背景与问题

当前生态中同时存在 HwjCode、Claude Code、Codex CLI、Pi、飞书/其他 AI 应用，以及多种独立的模型服务。每个应用单独维护 Provider 配置会带来：

- 同一组 API Key、模型额度和代理配置被重复维护；
- 不同应用使用不同协议，模型切换和故障转移不一致；
- 文本模型、视觉模型、工具调用模型的能力边界容易被误判；
- Provider 出错时，上层应用只能看到失败，无法统一执行 fallback、熔断和恢复；
- 请求量、延迟、错误和模型使用情况分散在不同应用；
- 高价值的 AI 对话无法统一归档、清洗和检索；
- 管理、部署、升级和移动端运维缺少统一入口。

因此，本项目需要从“API 网关”升级为“个人 AI 基础设施控制平面 + 数据平面 + 知识数据出口”。

## 2. 产品目标

### 2.1 核心目标

1. **统一入口**：所有 AI 应用使用一个端点、一套认证和统一模型目录。
2. **协议兼容**：兼容 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages，并保留多模态、工具调用、推理和流式能力。
3. **能力感知路由**：根据模型声明的 capabilities 和 input_modalities 选择正确 Provider，避免把图片请求路由到文本模型。
4. **可靠性编排**：支持 Provider 优先级、自动 fallback、健康探测、熔断、半开恢复和运行时启停。
5. **统一控制面**：通过 Admin API、Web Dashboard 和 Android App 查看并控制 Provider、模型、路由、健康和指标。
6. **可观测性**：统一记录请求 ID、协议、模型、Provider、状态、延迟、Token、流式状态和失败原因。
7. **对话资产化**：在个人部署场景下，支持受控的完整请求/响应归档，并为知识处理提供稳定导出接口。
8. **知识飞轮**：把网关对话与豆包、ChatGPT、Claude、Codex、Gemini 等其他来源汇入统一知识处理管道。
9. **简单运维**：单二进制、跨平台安装、声明式配置、可重复发布、健康检查和自动部署。

### 2.2 非目标

- 不负责训练、托管或替代任何大模型；
- 不把 Provider API Key 暴露给上层应用；
- 不做面向公众的多租户 SaaS，不在当前阶段引入复杂计费；
- 不在网关内部实现完整 Wiki、向量数据库或 LLM 编译器；
- 不维护第二套桌面客户端；当前正式管理端是 Web Dashboard 和 React Native Android App；
- 不让上层应用直接依赖某个 Provider 的私有请求格式；
- 不把所有原始对话默认上传到第三方服务。

## 3. 使用场景

### 3.1 AI 应用统一接入

Claude Code、Codex CLI、Pi、HwjCode、飞书内置 AI 能力和后续应用都通过网关访问模型。应用只需要知道：

- 网关地址；
- 一个 Bearer Key；
- /v1/models 返回的模型和能力；
- 所使用的协议端点。

### 3.2 文本与多模态请求

- 文本请求选择文本模型；
- 图片、视频、文件请求选择声明了对应能力的模型；
- 工具调用、推理、流式输出根据 Provider 能力路由；
- 当指定模型不满足请求能力时，返回明确错误或选择兼容模型，不静默降级为错误的文本模型。

### 3.3 Provider 故障恢复

当主 Provider 超时、限流、认证失效或服务异常时，网关根据路由链切换备用 Provider；连续失败触发熔断，恢复后通过半开探测自动恢复。

### 3.4 运行时管理

通过 Web 或 Android 管理端：

- 查看 Provider 和模型状态；
- 临时启停 Provider；
- 调整路由优先级；
- 重置熔断器；
- 手动健康探测；
- 查看请求、错误、延迟和 Token 统计；
- 查看版本、配置摘要和运行健康。

### 3.5 个人知识沉淀

网关作为对话数据入口，记录经过的 AI 使用事件。原始对话先进行脱敏、去重和筛选，再导出到 agent-lessons / hwj-wiki 等知识处理系统，形成知识卡、项目日志、决策记录和可检索索引。

## 4. 产品边界与分层

网关由四个边界清晰的层组成：

| 层 | 负责什么 | 不负责什么 |
|---|---|---|
| 数据平面 | 接收请求、协议转换、能力路由、Provider 转发、流式响应 | 知识分类和 Wiki 编译 |
| 控制平面 | Provider、模型、路由、健康、熔断、运行配置和权限管理 | 替上层应用保存业务状态 |
| 观测与归档层 | 指标、请求事件、可选的完整对话归档、导出 | 直接把全部数据发送给外部服务 |
| 知识数据出口 | 提供版本化 JSONL/CLI 导出，供知识管道消费 | 绑定某个知识库内部数据库表结构 |

核心边界原则：

1. 上层应用只依赖网关公开契约，不直接读取 Provider 配置或 SQLite；
2. Dashboard 和 Android App 只调用 Admin API，不绕过认证访问数据库；
3. request_logs 保持轻量指标用途，完整正文使用独立存储；
4. 知识库通过稳定导出契约接入，而不是直接耦合网关内部表结构；
5. Provider 列表的唯一事实来源是声明式配置和 /v1/models，文档不硬编码过期模型。

## 5. 总体架构

~~~mermaid
flowchart LR
    subgraph APPS[AI 应用层]
        CC[Claude Code]
        CX[Codex CLI]
        PI[Pi]
        HC[HwjCode]
        FEI[飞书及其他应用]
    end

    subgraph GW[LLM Gateway]
        AUTH[认证与请求 ID]
        DATA[数据平面]
        ROUTE[能力感知路由]
        CTRL[控制平面]
        OBS[指标与请求事件]
        ARCH[可选对话归档]
    end

    subgraph PROVIDERS[Provider 层]
        GLM[GLM / GLM Vision]
        ALI[阿里 MaaS]
        OPTIONAL[可选 Provider：Copilot / ChatGPT 等]
    end

    subgraph MGMT[管理端]
        WEB[Web Dashboard]
        ANDROID[React Native Android App]
    end

    subgraph KNOWLEDGE[知识处理层]
        EXPORT[版本化 JSONL / CLI Export]
        LESSONS[agent-lessons：采集、清洗、编译]
        WIKI[hwj-wiki personal：统一个人知识库]
        SEARCH[检索与可视化]
    end

    CC --> AUTH
    CX --> AUTH
    PI --> AUTH
    HC --> AUTH
    FEI --> AUTH
    AUTH --> DATA --> ROUTE
    ROUTE --> GLM
    ROUTE --> ALI
    ROUTE --> OPTIONAL
    CTRL --> ROUTE
    DATA --> OBS
    DATA --> ARCH
    WEB --> CTRL
    ANDROID --> CTRL
    ARCH --> EXPORT --> LESSONS --> WIKI --> SEARCH
~~~

## 6. 当前实现基线

以下是当前仓库的事实基线；未来 Provider 和模型以 go-gateway/providers.yaml、环境变量和 /v1/models 为准。

| 能力 | 当前状态 | 说明 |
|---|---|---|
| Go + Gin 单二进制网关 | ✅ 已实现 | 默认监听 :4001 |
| OpenAI Chat Completions | ✅ 已实现 | 支持流式和非流式 |
| OpenAI Responses API | ✅ 已实现 | 支持 Codex 兼容路径和流式 |
| Anthropic Messages | ✅ 已实现 | 支持流式和非流式 |
| Provider 声明式配置 | ✅ 已实现 | providers.yaml + 环境变量 |
| 模型别名和路由链 | ✅ 已实现 | 支持 model-bound Provider |
| 自动 fallback | ✅ 已实现 | 按链路顺序尝试 |
| 健康检查和熔断 | ✅ 已实现 | 支持恢复和运行时控制 |
| 能力感知模型目录 | ✅ 已实现 | /v1/models 返回能力和输入模态 |
| 多模态路由 | ✅ 已实现 | glm-vision 与文本模型区分 |
| SQLite 指标 | ✅ 已实现 | request_logs、daily_stats |
| Admin API | ✅ 已实现 | Provider、路由、模型、日志、统计 |
| Web Dashboard | ✅ 已实现 | web/，React + Vite |
| Android 管理端 | ✅ 已实现 | mobile-app/，React Native + Expo |
| 跨平台安装和发布 | ✅ 已实现 | Unix、Windows、macOS、Linux 构建 |
| 远程自动部署 | ✅ 已实现 | GitHub Actions + 健康检查 |
| 完整请求/响应持久化 | ⚠️ 部分实现 | 当前仅有控制台原始请求日志和指标，未形成归档数据模型 |
| 流式响应完整归档 | ❌ 未实现 | 需要聚合 SSE 事件并记录终态 |
| 网关知识库 Connector | ❌ 未实现 | 需要稳定导出契约 |
| 多源统一检索 | 🟡 规划中 | agent-lessons、hwj-wiki、pi-go 仍需明确分工 |

## 7. 功能需求

### FR-001：统一访问入口

- 所有上层应用通过一个网关地址访问模型；
- 使用 Bearer 认证，Provider 凭据只存在网关配置中；
- 支持 /v1 标准路径和必要的短路径兼容别名；
- 每个请求生成稳定的 request_id，通过响应头和日志关联。

验收标准：

- 同一个 Key 可以访问三个协议端点；
- 未认证请求被拒绝；
- 上层应用不需要知道 Provider API Key；
- 请求 ID 可以贯穿网关日志、Provider 尝试和归档记录。

### FR-002：协议和内容兼容

- Chat Completions、Responses、Messages 的基本字段正确转换；
- 保留工具调用、推理内容、扩展字段和流式事件；
- 支持文本、图片、视频、文件等内容块；
- 对不支持的字段返回可诊断错误，不静默丢失。

### FR-003：模型目录与能力契约

GET /v1/models 是客户端模型发现和能力选择的正式入口，至少提供：

- 对外模型 ID 和别名；
- Provider 和实际绑定模型；
- capabilities：text、vision、video、file、tool_calling、streaming、reasoning；
- input_modalities；
- 协议类型；
- 上下文和输出限制（如果 Provider 能可靠提供）。

客户端不得把某个 Provider 的单一模型硬编码为所有图片场景的默认值。图片请求应根据能力字段选择 glm-vision 或其他兼容模型。

### FR-004：路由、fallback 与熔断

- 支持显式模型绑定和模型链；
- 根据能力先筛选，再执行 Provider 优先级；
- 对可重试错误执行 fallback；
- 对认证、参数、能力不匹配等不可重试错误快速失败；
- 连续失败触发熔断，恢复后半开探测；
- 记录每次 Provider 尝试及最终结果；
- 不因 fallback 把一次用户请求统计成多次逻辑请求。

### FR-005：Provider 控制平面

Admin API 必须支持：

- 查看 Provider 运行状态、最近探测、成功率和熔断状态；
- 运行时启用/禁用 Provider；
- 调整模型路由顺序；
- 手动重置熔断器；
- 触发健康探测；
- 查看模型能力和协议摘要；
- 查看配置来源，但永远不返回明文 API Key。

Provider 配置应采用声明式文件 + 环境变量，新增 Provider 尽量不修改核心路由代码。

### FR-006：指标与请求事件

指标数据与正文数据分离。

#### 当前指标表：request_logs

仅保存：

- 时间、HTTP 方法、路径；
- 请求模型、最终 Provider；
- 状态码、延迟、输入/输出 Token；
- 是否流式；
- 规范化错误摘要。

#### 未来请求事件

增加逻辑请求与 Provider 尝试的关联：

- request_id；
- 请求协议和客户端来源；
- 逻辑请求状态；
- Provider 尝试顺序、状态、耗时和失败原因；
- fallback 次数；
- 终态和 Token。

指标表不能直接膨胀为大正文表，也不能依赖控制台 stdout 作为历史存储。

### FR-007：受控对话归档

个人部署可以启用完整对话归档，但必须是可配置、可审计、可清理的独立能力。

建议数据模型：

| 数据集 | 用途 |
|---|---|
| conversation_records | 一次逻辑对话请求的元数据和状态 |
| conversation_messages | 规范化后的用户、工具和助手消息 |
| conversation_payloads | 脱敏后的原始请求/响应或压缩引用 |
| provider_attempts | 每个 Provider 尝试和 fallback 过程 |

必须满足：

- Chat、Messages、Responses 三条路径统一捕获；
- 非流式保存完整响应；
- 流式聚合 SSE 后保存终态，同时保留错误和中断状态；
- ChatGPT passthrough 等特殊路径也必须进入同一归档契约；
- 请求和响应头中的 API Key、Cookie、Token、密码永不落库；
- JSON 中疑似密钥字段递归脱敏；
- 提供最大正文大小、压缩、保留天数和按来源清理；
- 本地文件权限默认最小化，远程部署必须有额外认证；
- 归档失败不能影响主请求成功返回，但必须有可观测告警。

#### 多模态数据策略

图片、音频、视频和文件可能以 URL、文件引用或 Base64 内联数据出现。默认策略：

- 归档正文中用 MIME、大小、哈希和引用替换超大的内联二进制；
- 图片识别结果和 OCR 文本可以作为可检索正文保存；
- 原始二进制仅在用户显式开启并设置保留策略时保存；
- 不把 Base64 图片直接写入 request_logs 或无限增长的 SQLite 单行。

### FR-008：知识库导出与知识飞轮

网关不实现知识编译，而是提供稳定的导出能力：

~~~text
gateway conversations export
  --since <timestamp>
  --until <timestamp>
  --format jsonl
  --redact
  --cursor <cursor>
~~~

导出记录至少包括：

- source = gateway；
- request_id、时间、应用/协议、模型和 Provider；
- 脱敏后的消息；
- 工具调用和结果；
- 状态、错误、Token 和延迟；
- 导出版本和游标。

推荐职责分工：

- **LLM Gateway**：采集、保护、归档、导出；
- **agent-lessons**：多源采集、噪音过滤、LLM 编译、去重和分类；
- **hwj-wiki personal**：个人知识库、OKF 输出、节点图和知识检索；
- **pi-go .llm-wiki**：代码事实和项目级技术知识；
- 统一检索：在上述边界稳定后再提供聚合搜索，不提前把三个系统强行合并。

知识处理链：

~~~text
原始来源
  → 网关/文件/平台 Connector
  → 脱敏与去噪
  → 去重与质量门
  → 知识卡、项目日志、决策记录
  → 标签/关键词/向量索引
  → AI 检索与可视化
~~~

### FR-009：Web 与 Android 管理端

#### Web Dashboard

web/ 是正式 Web 管理端，负责：

- Dashboard 概览；
- Provider 状态；
- 模型和路由；
- 请求日志和统计；
- 基础配置；
- 健康检查和错误诊断。

#### Android App

mobile-app/ 是唯一正式 Android 客户端，采用 React Native + Expo：

- 复用 Admin API；
- 支持服务器地址、认证和本地配置；
- 查看 Dashboard、Logs、Models、Providers；
- 适配移动端网络和认证失败提示；
- 通过 Android CI 生成 APK。

不再维护 desktop/、旧 Capacitor Android 工程或其他静态 UI mockup。

### FR-010：部署、升级与恢复

- 单二进制运行，运行时不依赖 Go/Node；
- 安装脚本支持 macOS、Linux、Windows；
- GitHub Actions 自动执行 Go 测试、安装器回归、跨平台构建和发布；
- 支持本地、服务器和小型个人设备部署；
- 配置与密钥分离，升级不覆盖用户配置；
- 健康检查用于启动验证、部署验证和移动端连接诊断；
- 数据目录、指标库和归档库可备份和恢复；
- 迁移必须幂等，旧版本数据不能因升级直接丢失。

## 8. 安全与数据治理

### 8.1 密钥安全

- API Key、OAuth Token、Admin Token 只能来自环境变量、受保护配置或 Secret 管理；
- 禁止把密钥写入 PRD、README、Issue、日志、请求正文或 Git 历史；
- Dashboard 只展示脱敏后的 Provider 信息；
- 安装向导输入密钥时必须掩码；
- 发现疑似泄露时立即撤销并轮换，而不是只删除文本。

### 8.2 访问控制

当前优先支持单用户/个人部署：

- 数据平面使用 Bearer Key；
- 控制平面使用 Admin Token；
- /health 可匿名访问，其他端点默认认证；
- 远程部署推荐 TLS 或反向代理；
- 不把 Admin API 暴露到公网而没有额外访问控制。

未来如出现多用户需求，再引入角色、租户和审计，不提前增加复杂度。

### 8.3 对话数据

- 完整归档默认只面向用户明确开启的个人部署；
- 归档数据有独立保留策略；
- 原始对话与知识卡分离，知识卡可单独删除；
- 导出前再次脱敏；
- 删除操作应提供按时间、来源和请求 ID 的精确范围；
- 归档失败、脱敏失败或超过大小限制都必须可观测。

## 9. 数据与 API 契约

### 9.1 数据平面

| 端点 | 作用 |
|---|---|
| GET /health | 存活检查 |
| GET /v1/models | 模型、能力和协议目录 |
| POST /v1/chat/completions | OpenAI Chat Completions |
| POST /v1/responses | OpenAI Responses |
| POST /v1/messages | Anthropic Messages |

### 9.2 控制平面

| 端点 | 作用 |
|---|---|
| GET /admin/dashboard | Dashboard 概览 |
| GET /admin/providers | Provider 状态 |
| PATCH /admin/providers/:name | 启停 Provider |
| POST /admin/providers/:name/reset | 重置熔断 |
| POST /admin/providers/:name/health-check | 健康探测 |
| GET /admin/models | 模型目录 |
| PUT /admin/models/:model | 更新模型能力 |
| GET /admin/routes | 查看路由链 |
| PUT /admin/routes/:model | 调整路由顺序 |
| GET /admin/logs | 请求指标日志 |
| GET /admin/stats | 聚合统计 |

### 9.3 未来归档与导出

归档 API 或 CLI 必须：

- 使用游标而不是依赖 SQLite 自增 ID；
- 支持时间范围、来源、模型和状态过滤；
- 返回 schema 版本；
- 保证重复导出可去重；
- 导出内容默认已脱敏；
- 不把 Provider 私有字段泄漏给下游。

## 10. 关键验收标准

### 10.1 数据平面

- 三种协议都能完成非流式和流式请求；
- 工具调用、推理和多模态内容不被静默丢失；
- 图片请求不会错误路由到只支持文本的模型；
- GET /v1/models 能反映实际可用模型和能力。

### 10.2 可靠性

- 主 Provider 失败时可按策略 fallback；
- 不可重试错误不触发无意义重试；
- 熔断、半开和恢复状态可观测；
- fallback 后客户端仍获得正确协议格式；
- Provider 最终状态和逻辑请求状态分别可查询。

### 10.3 控制面

- 未认证访问 Admin API 返回 401；
- Provider 启停、路由调整和熔断重置立即生效；
- API Key 不出现在任何管理响应；
- Web 和 Android 使用同一套 Admin API 契约。

### 10.4 归档与知识

- Chat、Messages、Responses 都能生成同一 schema 的归档记录；
- 流式请求能记录完整终态或明确中断原因；
- 脱敏测试覆盖 Authorization、Token、密码和常见密钥字段；
- 大型多媒体请求不会拖垮指标表；
- 导出可断点续跑、幂等，并能被知识管道消费；
- 归档数据经清洗后能生成知识卡或项目日志。

### 10.5 运维

- 跨平台构建和安装器回归通过；
- 新版本升级不覆盖用户 .env 和 Provider 配置；
- 部署后健康检查通过；
- 数据库迁移和备份恢复有最小回归测试；
- Android APK 和 Web Dashboard 的版本与网关 API 兼容。

## 11. 里程碑

### P0：基础设施基线

- 以 providers.yaml 和 /v1/models 作为模型事实来源；
- 补齐 request ID、最终 Provider 和 Provider attempt 记录；
- 修正 Dashboard 认证、错误响应和日志可诊断性；
- 保持指标表轻量，增加 schema/migration 机制；
- 更新 README、部署文档和示例配置，删除已废弃 Provider。

### P1：对话归档

- 新建独立 conversation 数据模型；
- 覆盖三个协议、非流式、流式和 passthrough；
- 实现脱敏、大小限制、压缩、保留和清理；
- 增加归档失败不影响主请求的回归测试；
- 在本地个人部署中提供明确的启用开关。

### P1：知识出口

- 实现 JSONL/CLI 导出；
- 设计版本化 schema 和增量游标；
- 增加 gateway connector 或独立导入脚本；
- 接入 agent-lessons 编译流程；
- 将结果导入 hwj-wiki personal。

### P2：统一知识检索

- 定义 agent-lessons、hwj-wiki 和 pi-go 的唯一职责；
- 合并关键词、标签和向量检索入口；
- 增加项目、决策、经验和对话来源过滤；
- 将知识图谱/节点图提供给 Web 管理端或独立页面。

### P3：可选扩展

- 多实例部署和远程集中控制；
- Prometheus/OpenTelemetry 等标准观测接口；
- 多用户角色和审计；
- 对话归档加密和对象存储；
- 更丰富的 Provider 计费、配额和成本分析。

## 12. 产品指标

| 指标 | 目标 |
|---|---|
| 网关可用性 | 个人部署场景下稳定运行，部署健康检查成功 |
| 路由正确率 | 请求能力与 Provider 能力匹配，不发生静默错路由 |
| fallback 成功率 | 可重试故障能够切换到健康 Provider |
| 认证安全 | 无明文密钥进入日志、数据库或发布产物 |
| 归档完整率 | 启用归档后，三种协议的终态记录可追溯 |
| 导出可靠性 | 支持断点续跑、幂等和重复去重 |
| 管理一致性 | Web、Android 和 CLI 看到同一份模型/Provider 状态 |
| 文档一致性 | README、PRD、Provider 配置和 /v1/models 不出现长期冲突 |

## 13. 关键决策记录

1. 网关是个人 AI 基础设施，不只是 API 代理。
2. 数据平面、控制平面、观测归档和知识处理必须分层。
3. request_logs 不承载完整正文；正文使用独立归档模型。
4. 完整对话归档面向个人部署，但必须有脱敏、保留和清理边界。
5. hwj-wiki 是个人知识库目标出口，agent-lessons 负责采集和编译，pi-go .llm-wiki 负责代码事实。
6. web/ 和 mobile-app/ 是正式管理端，不维护桌面 mockup 或旧 Android 工程。
7. Provider 和模型以配置、能力元数据和 /v1/models 为准，客户端不得硬编码单一 Provider 模型。
8. 先完成个人单机闭环，再考虑多用户、分布式和成本计费。

## 14. 变更历史

| 版本 | 日期 | 变更 |
|---|---|---|
| 1.x | 早期 | 以 Claude Code 多 Provider API fallback 为主要目标 |
| 2.0 | 2026-08-08 | 升级为个人 AI 基础设施 PRD，加入控制平面、能力路由、可观测性、对话归档和知识库出口 |

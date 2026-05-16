# OpenAI-Compatible Provider Notes

> 整理时间：2026-05-16
> 范围：智谱 GLM / 小米 MiMo / 美团 LongCat 的 OpenAI 风格接口
> 说明：当前环境下，三家官网文档页面抓取并不稳定。本文档区分为两类信息：
> - **直接确认**：来自仓库现有代码或可直接获取的页面内容
> - **搜索摘要**：来自搜索结果中的官方文档摘要，需在实际接入前再次人工点开确认

---

## 1. 结论摘要

这三家都**提供 OpenAI 兼容接口**，至少覆盖：

- `POST /v1/chat/completions`
- `Authorization: Bearer <API_KEY>` 或等价 API Key 认证
- `model`
- `messages`
- `max_tokens`
- `stream`

但这不等于当前网关内部就已经是 OpenAI 协议主干。

当前项目 `go-gateway` 的内部统一请求结构仍然是 **Anthropic 风格**：

- 对外原始入口：`/v1/messages`
- 内部主请求结构：`internal/provider/types.go` 中的 `Request`
- 现有路由与 fallback 都围绕这套结构实现

所以：

- **上游** 这三家可以直接走 OpenAI 风格
- **网关内部** 如果不重构主干，仍需要一层 OpenAI -> 内部结构 的适配

---

## 2. 智谱 GLM

### 2.1 当前项目中可直接确认的信息

来自：`go-gateway/main.go`

项目当前已经同时使用了智谱的两种风格接口：

1. Anthropic 风格：
   - `https://open.bigmodel.cn/api/anthropic/v1/messages`
2. OpenAI 风格：
   - `https://open.bigmodel.cn/api/paas/v4/chat/completions`

这说明：

- 智谱在当前项目语境下，**明确支持 OpenAI chat completions 风格**
- 项目里 `glm-free` provider 已经在使用这个 OpenAI 风格上游地址

代码依据：

- `go-gateway/main.go`

### 2.2 搜索摘要中的官方信息

搜索结果显示：

- Base URL：`https://open.bigmodel.cn/api/paas/v4/`
- Chat Completions Endpoint：`https://open.bigmodel.cn/api/paas/v4/chat/completions`
- 兼容 OpenAI SDK
- 可通过 OpenAI SDK 设置：
  - `base_url="https://open.bigmodel.cn/api/paas/v4/"`
  - `api_key=<GLM_API_KEY>`
- 支持常见字段：
  - `model`
  - `messages`
  - `temperature`
  - `stream`

### 2.3 当前可采信结论

**可较高置信度认定：智谱支持标准 OpenAI 风格 chat completions。**

推荐记录：

- Base URL：`https://open.bigmodel.cn/api/paas/v4`
- Endpoint：`/chat/completions`
- 完整地址：`https://open.bigmodel.cn/api/paas/v4/chat/completions`

---

## 3. 小米 MiMo

### 3.1 搜索摘要中的官方信息

搜索结果显示：

- 小米 MiMo API Open Platform 兼容 OpenAI API 格式
- 可使用现有 OpenAI SDK
- Base URL 可能有两套：
  1. 按量计费：`https://api.xiaomimimo.com/v1`
  2. Token Plan：`https://token-plan-cn.xiaomimimo.com/v1`
- 文档说明中包含 OpenAI 兼容页面
- 支持模型包括：
  - `mimo-v2.5`
  - `mimo-v2.5-pro`
  - `mimo-v2-flash`
  - 等

### 3.2 当前项目中可直接确认的信息

来自：`go-gateway/main.go`

当前项目接入的是 MiMo 的 Anthropic 风格入口：

- `https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages`

这说明：

- 项目当前并没有直接使用 MiMo 的 OpenAI 风格入口
- 但从搜索摘要看，MiMo 官方应当提供了 `/v1` 下的 OpenAI 兼容接口

### 3.3 当前可采信结论

**较高概率支持 OpenAI 风格，但需要你或我后续人工再次打开官方文档最终确认 endpoint 细节。**

目前建议先记录为：

- Base URL（按量）：`https://api.xiaomimimo.com/v1`
- Base URL（Token Plan）：`https://token-plan-cn.xiaomimimo.com/v1`
- 预期 Endpoint：`/chat/completions`
- 预期完整地址：
  - `https://api.xiaomimimo.com/v1/chat/completions`
  - `https://token-plan-cn.xiaomimimo.com/v1/chat/completions`

> 注：以上 URL 目前是基于搜索摘要整理，不算本轮直接抓取确认。

---

## 4. 美团 LongCat

### 4.1 搜索摘要中的官方信息

搜索结果显示：

- LongCat API Platform 完全兼容 OpenAI API 规范
- 支持 `/v1/chat/completions`
- OpenAI 风格 base URL：`https://api.longcat.chat/openai`
- 使用 Bearer Token 认证

### 4.2 当前项目中可直接确认的信息

来自：`go-gateway/main.go`

当前项目里接入 LongCat 的方式是 Anthropic 风格：

- `https://api.longcat.chat/anthropic/v1/messages`

这说明：

- 项目当前没有直接走 LongCat 的 OpenAI 风格上游
- 但搜索摘要表明官方提供 OpenAI 兼容基址

### 4.3 当前可采信结论

目前建议先记录为：

- Base URL：`https://api.longcat.chat/openai`
- 若其 base_url 语义与 OpenAI SDK 一致，则 endpoint 为：`/chat/completions`
- 组合后的完整请求地址大概率为：
  - `https://api.longcat.chat/openai/chat/completions`

> 注：这里最容易混淆的是“base_url”与“完整 endpoint”拼接方式。当前轮次未直接抓到官方页面正文，因此这条需要后续人工二次确认。

---

## 5. 为什么这不自动等于“网关里不用转换”

关键区别在于 **上游协议** 和 **网关内部统一协议** 不是一回事。

### 5.1 目前项目内部事实

当前 `go-gateway` 内部统一的是 Anthropic 风格：

- `internal/provider/types.go`
  - `Request`
  - `Response`
- `internal/handlers/messages.go`
  - 处理 `/v1/messages`
- `internal/provider/router.go`
  - 按内部 `Request` / `Response` 做统一分发

### 5.2 现有项目其实已经在做协议转换

例如：`internal/provider/openai.go`

它的职责就是：

- 把内部 Anthropic 风格请求转成 OpenAI 风格上游请求
- 调用 OpenAI 风格上游 `/chat/completions`
- 再把 OpenAI 风格响应转回内部 Anthropic 风格

所以当前系统架构并不是：

- “内部天然就是 OpenAI”

而是：

- “内部统一一套结构，再适配不同上游协议”

### 5.3 因此要真正做到“完全不转换”，只有两条路

#### 路线 A：维持现状，只在入口/出口做适配

优点：
- 改动小
- 不破坏现有 `/v1/messages`
- 不影响现有 fallback 机制

缺点：
- 仍然存在 OpenAI <-> 内部结构 的转换

#### 路线 B：把整个网关内部主干重构为 OpenAI 风格

优点：
- 对接智谱/小米/LongCat/EasyClaw 这类 OpenAI 上游更直接
- 新增 `/v1/chat/completions` 更自然

缺点：
- 改动面大
- 现有 `/v1/messages`、Anthropic provider、流式 SSE、工具调用格式都要重新梳理
- 风险明显更高

---

## 6. 当前建议

如果目标是：

- 尽快补齐对外 `POST /v1/chat/completions`
- 不大改现有网关结构

那么建议继续使用：

- **对外 OpenAI 兼容接口**
- **内部保留现有统一结构**
- **上游若本来就是 OpenAI 风格，直接走 OpenAI provider**

这已经是当前项目里最稳妥的路径。

如果目标变成：

- 以后主要服务 OpenAI 风格客户端
- Anthropic 风格只是兼容层

那才值得考虑把内部主干逐步重构为 OpenAI 风格。

---

## 7. 当前项目里的直接代码证据

- `go-gateway/main.go`
  - 智谱 Anthropic：`https://open.bigmodel.cn/api/anthropic/v1/messages`
  - 智谱 OpenAI：`https://open.bigmodel.cn/api/paas/v4/chat/completions`
  - MiMo Anthropic：`https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages`
  - LongCat Anthropic：`https://api.longcat.chat/anthropic/v1/messages`
  - EasyClaw OpenAI：`https://api.easyclaw.work/v1/chat/completions`
- `go-gateway/internal/provider/openai.go`
  - 已存在 Anthropic -> OpenAI 上游的转换逻辑
- `go-gateway/internal/provider/types.go`
  - 当前内部统一请求/响应结构不是 OpenAI chat completions 原生格式

---

## 8. 后续动作建议

可选两条：

### 方案 1：继续当前最小改动路线

- 保留现有内部 Anthropic 统一结构
- 完成 `/v1/chat/completions` 非流式 + 流式
- 上游能直接走 OpenAI 的 provider 继续走 OpenAI

### 方案 2：先做一轮架构调整再继续

- 评估将内部主干改为 OpenAI 风格
- `/v1/messages` 改为兼容层
- provider 按 OpenAI/Anthropic 双协议重新分层

---

## 9. 待人工二次确认项

以下内容本轮未能直接抓到官方文档正文，建议你后续人工点开页面再确认：

1. 小米 MiMo 的最终 OpenAI base URL 是否以：
   - `https://api.xiaomimimo.com/v1`
   - 或 `https://token-plan-cn.xiaomimimo.com/v1`
   为准
2. LongCat 文档中的 `https://api.longcat.chat/openai` 是：
   - base_url
   - 还是完整 endpoint 前缀
3. 三家在 `stream=true` 时返回的 chunk 字段是否完全与 OpenAI 标准一致
4. tools / tool_calls / multimodal 字段是否存在厂商差异

---

## 10. 现阶段可操作结论

如果只是问：

“这三家是不是都有 OpenAI 风格接口？”

当前可以回答：

- **是，至少智谱可以直接确认；小米和 LongCat 从官方搜索摘要看也支持 OpenAI 兼容接口。**

如果进一步问：

“那我们的网关是不是就完全不需要做协议转换？”

当前答案是：

- **不是。除非你打算把网关内部主干整体重构为 OpenAI 风格，否则仍然需要在网关入口/出口保留一层适配。**

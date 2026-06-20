# litellm-gateway: Easy Code 自定义模型集成修复指南

## 问题背景

Easy Code 将 litellm-gateway 作为 "OpenAI Compatible" 自定义模型使用，发送 `/v1/chat/completions` 请求。当前网关收到 Easy Code 的请求后报 400 `Invalid request body`。

## Easy Code 发给网关的请求结构

```json
{
  "model": "my/LongCat-2.0-Preview",
  "messages": [
    {"role": "system", "content": "You are an AI assistant..."},
    {"role": "user", "content": "hello"}
  ],
  "tools": [{...}],
  "stream": true,
  "stream_options": {"include_usage": true},
  "max_tokens": 32768
}
```

## 网关请求流路径

```
Easy Code → gateway /v1/chat/completions
  → openAIChatCompletionsHandler.Handle()
    → toProviderRequest()  # OpenAI格式 → Anthropic内部格式
    → Router → OpenAIProvider.ForwardStream()
      → toOpenAIRequest()  # Anthropic内部格式 → OpenAI格式（发给上游）
        → 上游 LongCat/MiMo/GLM API
```

## 需要修复的问题

### 1. 【严重】System 消息被静默丢弃

**位置**: `internal/handlers/chat_completions.go` → `toProviderRequest()` + `internal/provider/openai.go` → `toOpenAIRequest()`

**问题**: `toProviderRequest()` 把 system 消息提取出来存到 `req.raw["system"]`，不放入 messages 数组。但 `toOpenAIRequest()` 遍历 `req.Messages` 生成上游请求时，完全没有处理 `req.raw["system"]`，导致上游模型**收不到任何系统指令**。

**修复**: 在 `toOpenAIRequest()` 的 messages 生成逻辑中，**最前面**插入 system 消息：

```go
// 在 toOpenAIRequest() 函数中，var msgs []openAIMessage 之后，for 循环之前添加：

// 重建 system 消息（toProviderRequest 提取到了 raw.system）
if sysRaw, ok := req.raw["system"]; ok {
    var sysText string
    if err := json.Unmarshal(sysRaw, &sysText); err == nil && sysText != "" {
        msgs = append(msgs, openAIMessage{
            Role:    "system",
            Content: sysText,
        })
    }
}
```

### 2. 【中等】`stream_options` 虽被过滤但不安全

**位置**: `internal/handlers/chat_completions.go` → `toProviderRequest()`

**问题**: Easy Code 发送的 `stream_options: {include_usage: true}` 是 OpenAI 专有字段，虽然目前 `toOpenAIRequest()` 不提取它（侥幸没传出去），但 `toProviderRequest()` 把它存到了 `req.raw`。未来如果改了 raw 透传逻辑，可能会把该字段透传到上游直接导致 400。

**修复**: 在 `toProviderRequest()` 的 skip 列表中加入 `stream_options`：

```go
// 在 toProviderRequest() 的 raw 遍历处：
for key, raw := range req.raw {
    if key == "messages" || key == "tools" || key == "model" ||
       key == "max_tokens" || key == "stream" || key == "stream_options" {
        continue
    }
    // ...
}
```

### 3. 【中等】流式错误被吞掉，客户端收到空 SSE

**位置**: `internal/handlers/chat_completions.go` → `handleStream()` + `streamFromProvider()`

**问题**: `streamFromProvider()` 调用 `c.Writer.WriteHeader(http.StatusOK)` 后才尝试上游连接。如果上游全部失败，`handleStream()` 检查 `c.Writer.Written()` 为 true（因为 200 头已写），就**不返回任何错误**。Easy Code 收到 200 但流是空的。

**修复**: 用标记位替代 `c.Writer.Written()`：

```go
func (h *openAIChatCompletionsHandler) handleStream(c *gin.Context, req *provider.Request) {
    // ...
    anySuccess := false
    for _, p := range providerChain {
        if err := h.streamFromProvider(c, req, p); err == nil {
            anySuccess = true
            return
        }
        // ...
    }
    if !anySuccess {
        c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("all providers failed: %v", lastErr)})
    }
}
```

### 4. 【低】缺请求体调试 dump

**位置**: `internal/handlers/chat_completions.go` → `Handle()`

**问题**: 网关已经 log 了 raw body，但这个日志输出到网关自己的 stdout/stderr，Easy Code 用户看不到。

**修复**: debug 模式写入文件：

```go
import "os"

// 在 Handle() 中：
if os.Getenv("GATEWAY_DEBUG") == "1" {
    os.WriteFile("/tmp/gateway-debug-request.json", rawBody, 0644)
}
```

### 5. 【低】`max_tokens` 默认值

**位置**: `internal/provider/openai.go` → `toOpenAIRequest()`

**问题**: `openAIRequest.MaxTokens` 用了 `omitempty` tag，如果上游模型要求必须有 `max_tokens`，而该值为 0 时会跳过序列化。

**修复**:

```go
oaiReq := &openAIRequest{
    Model:    req.Model,
    Messages: msgs,
}
if req.MaxTokens > 0 {
    oaiReq.MaxTokens = req.MaxTokens
} else {
    oaiReq.MaxTokens = 32768
}
```

---

## 修复优先级

| 优先级 | 问题 | 影响 |
|--------|------|------|
| P0 | System 消息丢弃 | 模型没有系统指令，回答质量差 |
| P1 | 流式错误吞掉 | 用户看不到错误信息 |
| P1 | `stream_options` 显式过滤 | 防御性编程 |
| P2 | 调试 dump | 提高可调试性 |
| P2 | `max_tokens` 默认值 | 防止上游拒绝 |

---

## 验证方式

修复后，在 Easy Code 中验证：

```bash
DEEPV_DEBUG_CUSTOM_MODEL=1 hwjcode
```

然后 `/model custom:openai:coding@xxx` 切换到网关模型，发一句话测试。观察 Easy Code 输出的 `[CustomModel] Stream Request →` 日志，确认：
1. 网关正常返回 200
2. 流式内容正确输出
3. 模型遵循了 system 指令（说明 system 消息已正确传递）

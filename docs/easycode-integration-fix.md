# 网关修复要点速查

5 个问题，优先级从高到低：

## P0 - System 消息丢弃
`openai.go:toOpenAIRequest()` 没把 raw["system"] 重建为 message，上游模型收不到系统指令。

修改 `toOpenAIRequest()`，在遍历 messages 前插入：
```go
if sysRaw, ok := req.raw["system"]; ok {
    var sysText string
    json.Unmarshal(sysRaw, &sysText)
    msgs = append(msgs, openAIMessage{Role: "system", Content: sysText})
}
```

## P1 - stream_options 显式过滤
`chat_completions.go:toProviderRequest()` 的 skip 列表加 `"stream_options"`。

## P1 - 流式错误被吞
`handleStream()` 用 `c.Writer.Written()` 判断是否成功，但 200 header 提前写了。改用 `anySuccess` 标记位。

## P2 - 调试 dump
`GATEWAY_DEBUG=1` 时把原始请求体写入 `/tmp/gateway-debug-request.json`。

## P2 - max_tokens 默认值
`toOpenAIRequest()` 中 MaxTokens 为 0 时设默认 32768。

---

完整文档：`docs/easycode-integration-fix.md`

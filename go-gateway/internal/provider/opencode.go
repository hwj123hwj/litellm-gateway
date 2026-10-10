package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// OpenCodeProvider 是 OpenCode Zen（opencode.ai/zen）专用 provider。
//
// 上游是标准 OpenAI Chat Completions 协议，因此复用 OpenAIProvider 的全部协议
// 转换（Anthropic 请求 → OpenAI 请求、OpenAI SSE → Anthropic SSE）。唯一的差别在
// 于上游对「免费通道」的请求形态有硬性要求：
//
//	(a) 请求体必须 stream:true（stream:false 直接返回 403 FreeTierError）；
//	(b) 请求体必须带 tools 数组，且其中同时含 bash 与 read 两项；
//	(c) User-Agent 必须形如 opencode/<version>；
//	(d) 请求头 x-opencode-session 必须是 ses_ + 26 位 base62 字符。
//
// (a)(b) 由渠道的 Transform 补齐，(c)(d) 由渠道的请求头提供。provider 侧只需要
// 保证「即使下游要的是非流式响应，也走流式端点，再把 SSE 累积回完整响应」——
// 这正是 ForwardRequest 覆盖做的事。它沿用 TraeProvider 的既有模式
// （上游只提供流式端点时的标准处理），只是这里复用 OpenAIProvider 的
// ForwardStream 来做转换，不再自己解析私有 SSE。
type OpenCodeProvider struct {
	*OpenAIProvider
}

// NewOpenCodeProvider 用给定配置构造 OpenCode provider。
func NewOpenCodeProvider(config *Config) *OpenCodeProvider {
	return &OpenCodeProvider{OpenAIProvider: NewOpenAIProvider(config)}
}

// ForwardRequest 发一次非流式请求：内部走流式端点，把 Anthropic SSE 累积成完整响应。
//
// 为什么不直接复用 OpenAIProvider.ForwardRequest：那条路径把上游响应体当普通 JSON
// 解析，而 OpenCode 免费通道强制 stream:true，响应体是 SSE 文本，Unmarshal 必然失败。
func (p *OpenCodeProvider) ForwardRequest(ctx context.Context, req *Request) (*Response, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var buf bytes.Buffer
	if err := p.ForwardStream(streamCtx, req, &buf); err != nil {
		return nil, err
	}
	return anthropicSSEToResponse(buf.Bytes(), req.Model)
}

// anthropicSSEToResponse 把一段 Anthropic 格式 SSE 重新拼成完整 Response。
//
// 输入固定由本地 OpenAIProvider.ForwardStream 产出（网关自己生成的 SSE），因此
// 可以按网关的发射顺序严格解析，不必兼容任意上游。累积规则与 TraeProvider 一致：
// 同一类内容块（thinking / text / tool_use）按首次出现顺序落位，tool_use 的
// input_json_delta 追加到对应块的 Input 上。
func anthropicSSEToResponse(raw []byte, model string) (*Response, error) {
	var (
		blocks     []ContentBlock
		textBuf    strings.Builder
		thinkBuf   strings.Builder
		toolBlocks = map[int]int{} // SSE index → blocks 下标
		stopReason string
		usage      struct {
			input, output, cacheRead int
			cacheKnown               bool
		}
		msgID string
	)

	flushText := func() {
		if textBuf.Len() > 0 {
			blocks = append(blocks, ContentBlock{Type: "text", Text: textBuf.String()})
			textBuf.Reset()
		}
	}
	flushThink := func() {
		if thinkBuf.Len() > 0 {
			blocks = append(blocks, ContentBlock{Type: "thinking", Thinking: thinkBuf.String()})
			thinkBuf.Reset()
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "" {
			continue
		}
		var evt struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message struct {
				ID string `json:"id"`
			} `json:"message"`
			ContentBlock struct {
				Type     string          `json:"type"`
				ID       string          `json:"id"`
				Name     string          `json:"name"`
				Input    json.RawMessage `json:"input"`
				Thinking string          `json:"thinking"`
				Text     string          `json:"text"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Thinking    string `json:"thinking"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				InputTokens          *int `json:"input_tokens"`
				OutputTokens         *int `json:"output_tokens"`
				CacheReadInputTokens *int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			return nil, fmt.Errorf("parse opencode stream event: %w", err)
		}

		switch evt.Type {
		case "message_start":
			if evt.Message.ID != "" {
				msgID = evt.Message.ID
			}
		case "content_block_start":
			switch evt.ContentBlock.Type {
			case "tool_use":
				flushText()
				flushThink()
				if _, ok := toolBlocks[evt.Index]; ok {
					break
				}
				input := normalizeToolArgs(evt.ContentBlock.Input)
				toolBlocks[evt.Index] = len(blocks)
				blocks = append(blocks, ContentBlock{
					Type:  "tool_use",
					ID:    evt.ContentBlock.ID,
					Name:  evt.ContentBlock.Name,
					Input: input,
				})
			}
		case "content_block_delta":
			switch evt.Delta.Type {
			case "thinking_delta":
				// 与 TraeProvider 相同的纪律：任一时刻最多只有一个未落位的缓冲，
				// 切换块类型时先冲刷另一个，这样块顺序 = 上游实际到达顺序。
				flushText()
				thinkBuf.WriteString(evt.Delta.Thinking)
			case "text_delta":
				flushThink()
				textBuf.WriteString(evt.Delta.Text)
			case "input_json_delta":
				slot, ok := toolBlocks[evt.Index]
				if !ok || evt.Delta.PartialJSON == "" {
					break
				}
				blocks[slot].Input = appendOpenCodeToolArgs(blocks[slot].Input, evt.Delta.PartialJSON)
			}
		case "message_delta":
			if evt.Delta.StopReason != "" {
				stopReason = evt.Delta.StopReason
			}
			if evt.Usage.InputTokens != nil {
				usage.input = *evt.Usage.InputTokens
			}
			if evt.Usage.OutputTokens != nil {
				usage.output = *evt.Usage.OutputTokens
			}
			if evt.Usage.CacheReadInputTokens != nil {
				usage.cacheRead = *evt.Usage.CacheReadInputTokens
				usage.cacheKnown = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read opencode stream: %w", err)
	}
	flushText()
	flushThink()

	if len(blocks) == 0 {
		blocks = []ContentBlock{{Type: "text", Text: ""}}
	}
	if stopReason == "" {
		stopReason = "end_turn"
	}
	if msgID == "" {
		msgID = "msg_opencode_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}

	out := &Response{
		ID:         msgID,
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		StopReason: stopReason,
		Content:    blocks,
	}
	out.Usage.InputTokens = usage.input
	out.Usage.OutputTokens = usage.output
	if usage.cacheKnown {
		out.CacheReadInputTokens = usage.cacheRead
		out.CacheUsageKnown = true
		out.CacheInputTokens = usage.input + usage.cacheRead
	}
	return out, nil
}

// appendOpenCodeToolArgs 把一段 input_json_delta 追加到已有的工具参数上。
// 首个片段尚未构成合法 JSON（例如 "{}" 或空）时直接替换，否则做字符串拼接。
func appendOpenCodeToolArgs(existing json.RawMessage, delta string) json.RawMessage {
	if len(existing) == 0 || string(existing) == "{}" {
		return json.RawMessage(delta)
	}
	var buf bytes.Buffer
	buf.Write(existing)
	buf.WriteString(delta)
	return json.RawMessage(buf.Bytes())
}

// 让编译器检查 OpenCodeProvider 满足关键接口（含继承而来的方法）。
var (
	_ Provider       = (*OpenCodeProvider)(nil)
	_ StreamProvider = (*OpenCodeProvider)(nil)
)

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// openAIStreamTestServer 启动按序输出 SSE 帧的 OpenAI 兼容上游。
func openAIStreamTestServer(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			fmt.Fprintf(w, "data: %s\n\n", f)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func oaiChunk(delta string) string {
	return fmt.Sprintf(`{"id":"1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":%s,"finish_reason":null}]}`, delta)
}

func oaiFinish(reason string) string {
	return fmt.Sprintf(`{"id":"1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":%q}]}`, reason)
}

// TestForwardStreamConvertsReasoningContent：deepseek 系 reasoning_content 转成
// Anthropic thinking 块事件，且 block 序号按 thinking→text→tool_use 顺序连续分配。
func TestForwardStreamConvertsReasoningContent(t *testing.T) {
	srv := openAIStreamTestServer(t,
		oaiChunk(`{"role":"assistant"}`),
		oaiChunk(`{"reasoning_content":"想一想，"}`),
		oaiChunk(`{"reasoning_content":"需要先读文件。"}`),
		oaiChunk(`{"content":"我来读文件。"}`),
		oaiChunk(`{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.go\"}"}}]}`),
		oaiFinish("tool_calls"),
		"[DONE]",
	)
	p := NewOpenAIProvider(&Config{Name: "test", URL: srv.URL, APIKey: "k"})

	var out strings.Builder
	if err := p.ForwardStream(context.Background(), &Request{Model: "m"}, &out); err != nil {
		t.Fatalf("ForwardStream: %v", err)
	}
	got := out.String()

	// thinking 块：index 0，内容拼接无损
	if !strings.Contains(got, `"content_block_start","index":0,"content_block":{"type":"thinking"`) {
		t.Errorf("缺少 thinking 块 start 事件，输出:\n%s", got)
	}
	if !strings.Contains(got, `"thinking_delta","thinking":"想一想，"`) {
		t.Errorf("缺少第一条 thinking_delta，输出:\n%s", got)
	}
	if !strings.Contains(got, `"thinking_delta","thinking":"需要先读文件。"`) {
		t.Errorf("缺少第二条 thinking_delta，输出:\n%s", got)
	}

	// text 块紧随其后：index 1
	if !strings.Contains(got, `"content_block_start","index":1,"content_block":{"type":"text"`) {
		t.Errorf("text 块应在 index 1，输出:\n%s", got)
	}
	// tool_use 块：index 2
	if !strings.Contains(got, `"content_block_start","index":2,"content_block":{"type":"tool_use"`) {
		t.Errorf("tool_use 块应在 index 2，输出:\n%s", got)
	}
	// stop 事件覆盖三个块
	for _, idx := range []string{"0", "1", "2"} {
		if !strings.Contains(got, fmt.Sprintf(`"content_block_stop","index":%s`, idx)) {
			t.Errorf("缺少 index %s 的 content_block_stop，输出:\n%s", idx, got)
		}
	}
	// stop_reason 映射
	if !strings.Contains(got, `"stop_reason":"tool_use"`) {
		t.Errorf("缺少 stop_reason=tool_use，输出:\n%s", got)
	}
}

// TestForwardStreamIdleTimeout：上游停滞超过 RequestTimeout 时报错而非无限等待。
func TestForwardStreamIdleTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+oaiChunk(`{"content":"par"}`)+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // 停滞：直到客户端断开都不再发数据
	}))
	t.Cleanup(srv.Close)
	p := NewOpenAIProvider(&Config{Name: "test", URL: srv.URL, APIKey: "k", RequestTimeout: 300 * time.Millisecond})

	var out strings.Builder
	err := p.ForwardStream(context.Background(), &Request{Model: "m"}, &out)
	if err == nil {
		t.Fatalf("期望空闲超时错误，实际 nil；输出:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "idle") {
		t.Errorf("错误应说明空闲超时，实际: %v", err)
	}
	if !strings.Contains(out.String(), `"text":"par"`) {
		t.Errorf("停滞前收到的内容应已转发，输出:\n%s", out.String())
	}
}

// TestForwardStreamNoReasoningKeepsLegacyIndices：无 reasoning 时块序号与旧版
// 一致（text=0，tool 从 1 起），保证既有客户端不受影响。
func TestForwardStreamNoReasoningKeepsLegacyIndices(t *testing.T) {
	srv := openAIStreamTestServer(t,
		oaiChunk(`{"content":"hi"}`),
		oaiChunk(`{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]}`),
		oaiFinish("tool_calls"),
		"[DONE]",
	)
	p := NewOpenAIProvider(&Config{Name: "test", URL: srv.URL, APIKey: "k"})

	var out strings.Builder
	if err := p.ForwardStream(context.Background(), &Request{Model: "m"}, &out); err != nil {
		t.Fatalf("ForwardStream: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, `"content_block_start","index":0,"content_block":{"type":"text"`) {
		t.Errorf("text 块应在 index 0，输出:\n%s", got)
	}
	if !strings.Contains(got, `"content_block_start","index":1,"content_block":{"type":"tool_use"`) {
		t.Errorf("tool_use 块应在 index 1，输出:\n%s", got)
	}
	if strings.Contains(got, `"thinking"`) {
		t.Errorf("无 reasoning 时不应出现 thinking 块，输出:\n%s", got)
	}
}

// TestFromOpenAIResponseIncludesThinking：非流式响应的 reasoning_content
// 转成 thinking 块并置于 text 之前。
func TestFromOpenAIResponseIncludesThinking(t *testing.T) {
	oai := &openAIResponse{
		ID:    "resp_1",
		Model: "m",
		Choices: []openAIChoice{{
			Index: 0,
			Message: openAIMessage{
				Role:             "assistant",
				Content:          "答案是 42。",
				ReasoningContent: "先想想。",
			},
			FinishReason: "stop",
		}},
	}

	resp := fromOpenAIResponse(oai)

	if len(resp.Content) != 2 {
		t.Fatalf("应有 thinking+text 两个块，实际 %d: %+v", len(resp.Content), resp.Content)
	}
	if resp.Content[0].Type != "thinking" || resp.Content[0].Thinking != "先想想。" {
		t.Errorf("首块应为 thinking，实际: %+v", resp.Content[0])
	}
	if resp.Content[1].Type != "text" || resp.Content[1].Text != "答案是 42。" {
		t.Errorf("次块应为 text，实际: %+v", resp.Content[1])
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason 应为 end_turn，实际: %s", resp.StopReason)
	}
}

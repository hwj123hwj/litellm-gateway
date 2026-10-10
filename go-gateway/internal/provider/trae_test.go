package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 这组测试覆盖 Trae provider 的四件事：把 Anthropic 请求翻成 SOLO 请求体、
// 私有 SSE（event/data，空行分帧）翻回 Anthropic SSE、请求头族（含
// Authorization: Cloud-IDE-JWT）、以及未登录短路。
//
// 与 CloudCode 一样，Config.URL 指向假上游即可完全离线。

// traeTestAuth 是同时实现 AuthSource 与 traeIdentitySource 的测试令牌源，
// 用来验证 provider 会向渠道索取账号级身份头。
type traeTestAuth struct {
	token   string
	headers map[string]string
}

func (a *traeTestAuth) BearerToken() string                    { return a.token }
func (a *traeTestAuth) Refresh(context.Context) bool           { return false }
func (a *traeTestAuth) TraeIdentityHeaders() map[string]string { return a.headers }

// traeFakeServer 造一个假 SOLO 上游：把请求体解到 body（非 nil 时），头克隆到
// headers（非 nil 时），并按原样回写私有 SSE 帧。
func traeFakeServer(t *testing.T, frames []string, body *map[string]any, headers *http.Header) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body != nil {
			_ = json.NewDecoder(r.Body).Decode(body)
		}
		if headers != nil {
			*headers = r.Header.Clone()
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range frames {
			_, _ = io.WriteString(w, frame)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// traeFrame 拼一帧私有 SSE（event + data + 空行结尾）。
func traeFrame(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

func traeProviderForTest(t *testing.T, url, model string, auth AuthSource) *TraeProvider {
	t.Helper()
	return NewTraeProvider(&Config{Name: model, URL: url, Auth: auth})
}

// TestTraeBodyShape 验证 SOLO 请求体的关键形态：config_name 用剥前缀后的真名、
// function 固定、stream 恒为 true、system 独立成一条、工具 parameters 是字符串。
func TestTraeBodyShape(t *testing.T) {
	var got map[string]any
	server := traeFakeServer(t, []string{
		traeFrame("output", `{"response":"hi"}`),
		traeFrame("done", `{"finish_reason":"stop"}`),
	}, &got, nil)

	p := traeProviderForTest(t, server.URL, "trae/glm-5.2", NewStaticAuth("tok"))
	req := mustRequest(t, `{"model":"trae/glm-5.2","max_tokens":100000,"system":"sys prompt","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"lookup","description":"d","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}],"tool_choice":{"type":"auto"}}`)
	if _, err := p.ForwardRequest(context.Background(), req); err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}

	if got["config_name"] != "glm-5.2" {
		t.Fatalf("config_name 应为剥掉 trae/ 前缀的真名，得到 %v", got["config_name"])
	}
	if got["model"] != "glm-5.2" {
		t.Fatalf("model 应为剥掉前缀的真名，得到 %v", got["model"])
	}
	if got["stream"] != true {
		t.Fatalf("stream 应恒为 true，得到 %v", got["stream"])
	}
	if got["function"] != traeDefaultFunction {
		t.Fatalf("function 应为 %q，得到 %v", traeDefaultFunction, got["function"])
	}
	// max_tokens 被夹到官方硬上限。
	if got["max_tokens"] != float64(traeMaxCompletionTokens) {
		t.Fatalf("max_tokens 应被限制在 %d，得到 %v", traeMaxCompletionTokens, got["max_tokens"])
	}
	if got["tool_choice"] != "auto" {
		t.Fatalf("tool_choice 应为 auto，得到 %v", got["tool_choice"])
	}

	messages, _ := got["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages 应有 system + user 两条，得到 %d: %v", len(messages), messages)
	}
	sys, _ := messages[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "sys prompt" {
		t.Fatalf("首条应为 system 消息，得到 %v", sys)
	}
	user, _ := messages[1].(map[string]any)
	if user["role"] != "user" || user["content"] != "hello" {
		t.Fatalf("单段文本应压成字符串形态，得到 %v", user)
	}

	tools, _ := got["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools 应有 1 项，得到 %v", tools)
	}
	tool, _ := tools[0].(map[string]any)
	fn, _ := tool["function"].(map[string]any)
	// SOLO 要求 parameters 是 JSON 字符串，而不是对象。
	if _, ok := fn["parameters"].(string); !ok {
		t.Fatalf("parameters 应序列化成字符串，得到 %T (%v)", fn["parameters"], fn["parameters"])
	}
}

// TestTraeModelName 锁住模型名解析：剥前缀、按 `__` 截断、空值回退默认模型。
func TestTraeModelName(t *testing.T) {
	cases := map[string]string{
		"trae/glm-5.2":            "glm-5.2",
		"trae/kimi-k3":            "kimi-k3",
		"trae/minimax-m3__custom": "minimax-m3",
		"glm-5.2":                 "glm-5.2",
		"":                        traeDefaultModel,
		"trae/":                   traeDefaultModel,
	}
	for in, want := range cases {
		if got := traeModelName(in); got != want {
			t.Fatalf("traeModelName(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestTraeHeaderFamily 验证请求头族：鉴权是 Cloud-IDE-JWT（由渠道注入），
// 客户端身份头齐全，且不出现 applyAuth 会写出的 Bearer/x-api-key。
func TestTraeHeaderFamily(t *testing.T) {
	var headers http.Header
	server := traeFakeServer(t, []string{
		traeFrame("output", `{"response":"hi"}`),
		traeFrame("done", `{"finish_reason":"stop"}`),
	}, nil, &headers)

	auth := &traeTestAuth{token: "tok-1", headers: map[string]string{
		"Authorization":    "Cloud-IDE-JWT tok-1",
		"X-Cloudide-Token": "tok-1",
		"X-Ide-Token":      "tok-1",
		"X-Uid":            "u-1",
		"X-Machine-Id":     "machine-1",
		"X-Device-Id":      "device-1",
		"X-App-Id":         "app-1",
	}}
	p := traeProviderForTest(t, server.URL, "trae/glm-5.2", auth)

	req := mustRequest(t, `{"model":"trae/glm-5.2","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}

	if got := headers.Get("Authorization"); got != "Cloud-IDE-JWT tok-1" {
		t.Fatalf("鉴权头应为 Cloud-IDE-JWT，得到 %q", got)
	}
	if got := headers.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept 应为 text/event-stream，得到 %q", got)
	}
	for name, want := range auth.headers {
		if got := headers.Get(name); got != want {
			t.Fatalf("身份头 %s 应为 %q，得到 %q", name, want, got)
		}
	}
	for _, name := range []string{"X-Ide-Version", "X-App-Version", "X-Device-Type", "Request-Traffic-Type"} {
		if headers.Get(name) == "" {
			t.Fatalf("客户端身份头 %s 缺失", name)
		}
	}
}

// TestTraeStreamTranslatesToAnthropicSSE 验证流式路径：私有 SSE 被翻成 Anthropic
// SSE，文本与思考分别落到 text_delta / thinking_delta，用量进 message_delta。
func TestTraeStreamTranslatesToAnthropicSSE(t *testing.T) {
	server := traeFakeServer(t, []string{
		traeFrame("output", `{"reasoning_content":"think..."}`),
		traeFrame("output", `{"response":"hello"}`),
		traeFrame("output", `{"response":" world"}`),
		traeFrame("token_usage", `{"prompt_tokens":10,"completion_tokens":5}`),
		traeFrame("done", `{"finish_reason":"stop"}`),
	}, nil, nil)

	p := traeProviderForTest(t, server.URL, "trae/glm-5.2", NewStaticAuth("tok"))
	var sb strings.Builder
	req := mustRequest(t, `{"model":"trae/glm-5.2","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	if err := p.ForwardStream(context.Background(), req, &sb); err != nil {
		t.Fatalf("ForwardStream failed: %v", err)
	}
	out := sb.String()

	for _, want := range []string{
		`"type":"message_start"`, `"type":"content_block_start"`, `"type":"content_block_delta"`,
		`"type":"content_block_stop"`, `"type":"message_delta"`, `"type":"message_stop"`,
		`thinking_delta`, `text_delta`, `"stop_reason":"end_turn"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Anthropic SSE 缺少 %s，实际输出:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, " world") {
		t.Fatalf("文本增量未透传:\n%s", out)
	}
	if !strings.Contains(out, `"input_tokens":10`) || !strings.Contains(out, `"output_tokens":5`) {
		t.Fatalf("用量应进 message_delta:\n%s", out)
	}
}

// TestTraeSSEDataAppends 验证同一帧内的多条 data: 行是**追加**拼接（不是覆盖、
// 也不是各成独立帧）：拼出的 JSON 若合法则解析成一帧。
func TestTraeSSEDataAppends(t *testing.T) {
	frames := []string{"event: output\ndata: {\"response\":\"ok\",\ndata: \"extra\":1}\n\n"}

	p := traeProviderForTest(t, traeFakeServer(t, frames, nil, nil).URL, "trae/glm-5.2", NewStaticAuth("tok"))
	var sb strings.Builder
	req := mustRequest(t, `{"model":"trae/glm-5.2","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	if err := p.ForwardStream(context.Background(), req, &sb); err != nil {
		t.Fatalf("ForwardStream failed: %v", err)
	}
	if !strings.Contains(sb.String(), "ok") {
		t.Fatalf("追加拼接后应解析出 response，实际输出:\n%s", sb.String())
	}
}

// TestTraeStreamToolCalls 验证工具调用：分片参数按 index 累积成一个 input_json_delta
// 序列，且在有函数名之后才宣告 content_block_start。
func TestTraeStreamToolCalls(t *testing.T) {
	server := traeFakeServer(t, []string{
		traeFrame("output", `{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"lookup","arguments":"{\"q\":"}}]}`),
		traeFrame("output", `{"tool_calls":[{"index":0,"function":{"arguments":"\"hi\"}"}}]}`),
		traeFrame("done", `{"finish_reason":"tool_calls"}`),
	}, nil, nil)

	p := traeProviderForTest(t, server.URL, "trae/glm-5.2", NewStaticAuth("tok"))
	var sb strings.Builder
	req := mustRequest(t, `{"model":"trae/glm-5.2","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	if err := p.ForwardStream(context.Background(), req, &sb); err != nil {
		t.Fatalf("ForwardStream failed: %v", err)
	}
	out := sb.String()

	if !strings.Contains(out, `"type":"tool_use"`) || !strings.Contains(out, `"name":"lookup"`) {
		t.Fatalf("应产出 tool_use 块:\n%s", out)
	}
	if !strings.Contains(out, `input_json_delta`) {
		t.Fatalf("应有 input_json_delta:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("finish_reason=tool_calls 应映射成 tool_use:\n%s", out)
	}
}

// TestTraeForwardRequestAccumulates 验证非流式路径把私有 SSE 累积成完整 Response：
// 工具参数拼成合法 JSON，stop_reason 正确。
func TestTraeForwardRequestAccumulates(t *testing.T) {
	server := traeFakeServer(t, []string{
		traeFrame("output", `{"response":"answer"}`),
		traeFrame("output", `{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"lookup","arguments":"{\"q\":"}}]}`),
		traeFrame("output", `{"tool_calls":[{"index":0,"function":{"arguments":"\"hi\"}"}}]}`),
		traeFrame("token_usage", `{"prompt_tokens":3,"completion_tokens":4}`),
		traeFrame("done", `{"finish_reason":"tool_calls"}`),
	}, nil, nil)

	p := traeProviderForTest(t, server.URL, "trae/glm-5.2", NewStaticAuth("tok"))
	req := mustRequest(t, `{"model":"trae/glm-5.2","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := p.ForwardRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("stop_reason 应为 tool_use，得到 %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 3 || resp.Usage.OutputTokens != 4 {
		t.Fatalf("用量不正确: %+v", resp.Usage)
	}

	var text, toolArgs string
	var toolName string
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			text += block.Text
		case "tool_use":
			toolName = block.Name
			toolArgs = string(block.Input)
		}
	}
	if text != "answer" {
		t.Fatalf("文本块应为 answer，得到 %q", text)
	}
	if toolName != "lookup" {
		t.Fatalf("工具名应为 lookup，得到 %q", toolName)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(toolArgs), &parsed); err != nil {
		t.Fatalf("累积后的工具参数应是合法 JSON，得到 %q: %v", toolArgs, err)
	}
	if parsed["q"] != "hi" {
		t.Fatalf("工具参数不正确: %v", parsed)
	}
}

// TestTraeStreamErrorFrame 验证 error 事件被转成 ProviderError，且 4001 附带换模型指引。
func TestTraeStreamErrorFrame(t *testing.T) {
	server := traeFakeServer(t, []string{
		traeFrame("error", `{"code":4001,"message":"model not callable"}`),
	}, nil, nil)

	p := traeProviderForTest(t, server.URL, "trae/custom_model_x", NewStaticAuth("tok"))
	req := mustRequest(t, `{"model":"trae/custom_model_x","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	_, err := p.ForwardRequest(context.Background(), req)
	if err == nil {
		t.Fatalf("error 帧应导致失败")
	}
	providerErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("错误应为 *ProviderError，得到 %T", err)
	}
	if providerErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("状态码应为 502，得到 %d", providerErr.StatusCode)
	}
	if !strings.Contains(providerErr.Message, "4001") || !strings.Contains(providerErr.Message, "不可调用") {
		t.Fatalf("4001 应附带换模型指引，得到 %q", providerErr.Message)
	}
}

// TestTraeEmptyStreamFails 验证上游没给出任何事件时明确失败，而不是静默产出空回复。
func TestTraeEmptyStreamFails(t *testing.T) {
	server := traeFakeServer(t, nil, nil, nil)

	p := traeProviderForTest(t, server.URL, "trae/glm-5.2", NewStaticAuth("tok"))
	req := mustRequest(t, `{"model":"trae/glm-5.2","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err == nil {
		t.Fatalf("空响应应报错")
	}
}

// TestTraeUnavailableWhenLoggedOut 验证未登录时 Available=false 且不发请求。
func TestTraeUnavailableWhenLoggedOut(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewTraeProvider(&Config{Name: "trae/glm-5.2", URL: server.URL, Auth: NewStaticAuth("")})
	if p.Available() {
		t.Fatalf("未登录时 Available 应为 false")
	}
	req := mustRequest(t, `{"model":"trae/glm-5.2","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err == nil {
		t.Fatalf("未登录时应直接报错")
	}
	if hits != 0 {
		t.Fatalf("未登录时不应触达上游，实际 %d 次", hits)
	}
}

// TestTraeProviderRefreshOn401 验证 401 后 provider 会用账号池续期并重试一次。
func TestTraeProviderRefreshOn401(t *testing.T) {
	var auths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		if len(auths) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"expired"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, traeFrame("output", `{"response":"ok"}`))
		_, _ = io.WriteString(w, traeFrame("done", `{"finish_reason":"stop"}`))
	}))
	defer server.Close()

	auth := &traeTestAuth{token: "old-token", headers: map[string]string{"Authorization": "Cloud-IDE-JWT old-token"}}
	p := traeProviderForTest(t, server.URL, "trae/glm-5.2", auth)
	req := mustRequest(t, `{"model":"trae/glm-5.2","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	// 静态 Refresh 返回 false，这里只验证不 panic 且错误原样返回（真实续期由
	// channel 包的 TestTraeRefreshCredential 覆盖）。
	if _, err := p.ForwardRequest(context.Background(), req); err == nil {
		t.Fatalf("续期失败时应报错")
	}
	if len(auths) != 1 {
		t.Fatalf("续期失败不应重试，实际 %d 次", len(auths))
	}
}

// TestTraeStopReason 锁住 finish_reason 映射与工具调用推断。
func TestTraeStopReason(t *testing.T) {
	if got := traeStopReason("", true); got != "tool_use" {
		t.Fatalf("无 reason 但有工具调用应为 tool_use，得到 %q", got)
	}
	if got := traeStopReason("", false); got != "end_turn" {
		t.Fatalf("无 reason 且无工具调用应为 end_turn，得到 %q", got)
	}
	if got := traeStopReason("length", false); got != "max_tokens" {
		t.Fatalf("length 应映射成 max_tokens，得到 %q", got)
	}
	if got := traeStopReason("tool_calls", false); got != "tool_use" {
		t.Fatalf("tool_calls 应映射成 tool_use，得到 %q", got)
	}
	if got := traeStopReason("stop", false); got != "end_turn" {
		t.Fatalf("stop 应映射成 end_turn，得到 %q", got)
	}
}

// TestTraeToolChoice 验证 tool_choice 归一化。
func TestTraeToolChoice(t *testing.T) {
	cases := map[string]any{
		`{"type":"auto"}`:                 "auto",
		`{"type":"any"}`:                  "required",
		`{"type":"none"}`:                 "none",
		`{"type":"tool","name":"lookup"}`: "lookup",
	}
	for in, want := range cases {
		req := mustRequest(t, `{"model":"trae/glm-5.2","messages":[{"role":"user","content":"hi"}],"tool_choice":`+in+`}`)
		if got := traeToolChoice(req); got != want {
			t.Fatalf("tool_choice %s 应归一化为 %v，得到 %v", in, want, got)
		}
	}
}

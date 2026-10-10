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

// 这组测试覆盖 CloudCode provider 的三件核心事：把 Anthropic 请求翻成 CloudCode
// 信封、把 CloudCode SSE 翻回 Anthropic SSE、以及鉴权/未登录短路。
//
// 用 Config.URL 指向假上游（customEndpoint）时，loadCodeAssist 也只打这一个地址，
// 测试因此完全离线，不会碰到真实的沙箱/生产主机。

// cloudCodeFakeServer 造一个假 CloudCode 上游：loadCodeAssist 返回固定项目，流式
// 端点回一段可定制的 SSE。返回的 *httptest.Server 由调用方 Close。
func cloudCodeFakeServer(t *testing.T, project string, sseFrames []string, capture *map[string]any, captureHeaders *http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1internal:loadCodeAssist"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":` + strconvQuote(project) + `}`))
		case strings.HasSuffix(r.URL.Path, "/v1internal:streamGenerateContent"):
			if capture != nil {
				_ = json.NewDecoder(r.Body).Decode(capture)
			}
			if captureHeaders != nil {
				*captureHeaders = r.Header.Clone()
			}
			w.Header().Set("Content-Type", "text/event-stream")
			for _, frame := range sseFrames {
				_, _ = io.WriteString(w, "data: "+frame+"\n\n")
			}
		default:
			http.NotFound(w, r)
		}
	}))
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func cloudCodeProviderForTest(t *testing.T, url, model, token string) *CloudCodeProvider {
	t.Helper()
	return NewCloudCodeProvider(&Config{
		Name: model,
		URL:  url,
		Auth: NewStaticAuth(token),
	})
}

// mustRequest 从 JSON 体构造 *Request（走 UnmarshalJSON，填充 raw 字段）。
func mustRequest(t *testing.T, body string) *Request {
	t.Helper()
	var req Request
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return &req
}

// TestCloudCodeEnvelopeShape 验证请求信封的关键字段：model 为上游真名、project 已
// 注入、request.contents 里 assistant 变 model、system 单独进 systemInstruction。
func TestCloudCodeEnvelopeShape(t *testing.T) {
	var got map[string]any
	var gotHeaders http.Header
	server := cloudCodeFakeServer(t, "proj-42", []string{
		`{"response":{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}]}}`,
	}, &got, &gotHeaders)
	defer server.Close()

	p := cloudCodeProviderForTest(t, server.URL, "cloudcode/gemini-3.8-flash-high", "tok-1")

	req := mustRequest(t, `{"model":"cloudcode/gemini-3.8-flash-high","max_tokens":128,"system":"sys prompt","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"prev"},{"role":"user","content":"again"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}

	// 发往上游的模型名必须是上游真名，网关前缀应被剥掉。
	if got["model"] != "gemini-3.8-flash-high" {
		t.Fatalf("上游 model 应为 gemini-3.8-flash-high（剥掉 cloudcode/ 前缀），得到 %v", got["model"])
	}
	// project 来自 loadCodeAssist。
	if got["project"] != "proj-42" {
		t.Fatalf("project 应为 loadCodeAssist 返回值，得到 %v", got["project"])
	}
	// 鉴权头必须存在且带 cloudcode 前缀剥离后的 Bearer，同时复刻身份头。
	if gotHeaders.Get("Authorization") != "Bearer tok-1" {
		t.Fatalf("Authorization 应为 Bearer tok-1，得到 %q", gotHeaders.Get("Authorization"))
	}
	if gotHeaders.Get("x-client-name") != "antigravity" {
		t.Fatalf("应带复刻身份头 x-client-name=antigravity，得到 %q", gotHeaders.Get("x-client-name"))
	}

	inner, _ := got["request"].(map[string]any)
	if inner == nil {
		t.Fatalf("信封缺少 request 对象: %v", got)
	}
	// system 单独进 systemInstruction，不混进 contents。
	sys, _ := inner["systemInstruction"].(map[string]any)
	if sys == nil {
		t.Fatalf("system 应进 systemInstruction，得到 %v", inner["systemInstruction"])
	}
	contents, _ := inner["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents 应有 3 条（system 已剥离），得到 %d: %v", len(contents), contents)
	}
	second, _ := contents[1].(map[string]any)
	if second["role"] != "model" {
		t.Fatalf("assistant 消息应翻成 role=model，得到 %v", second["role"])
	}
	// 思考档决定 thinkingBudget。
	gen, _ := inner["generationConfig"].(map[string]any)
	thinking, _ := gen["thinkingConfig"].(map[string]any)
	if thinking["thinkingBudget"] != float64(10000) {
		t.Fatalf("high 档 thinkingBudget 应为 10000，得到 %v", thinking["thinkingBudget"])
	}
}

// TestCloudCodeModelSpecTiers 锁住档位解析：前缀剥离、默认 medium、-tiered 不给预算。
func TestCloudCodeModelSpecTiers(t *testing.T) {
	cases := []struct {
		in     string
		upst   string
		effort string
	}{
		{"cloudcode/gemini-3.8-flash-high", "gemini-3.8-flash-high", "high"},
		{"cloudcode/gemini-3.8-flash", "gemini-3.8-flash-medium", "medium"},
		{"gemini-3.8-flash-low", "gemini-3.8-flash-low", "low"},
		{"cloudcode/gemini-3.8-flash-tiered", "gemini-3.8-flash-tiered", "tiered"},
	}
	for _, c := range cases {
		upstream, effort := cloudCodeModelSpec(c.in)
		if upstream != c.upst || effort != c.effort {
			t.Fatalf("cloudCodeModelSpec(%q) = (%q,%q)，期望 (%q,%q)", c.in, upstream, effort, c.upst, c.effort)
		}
	}
}

// TestCloudCodeStreamTranslatesToAnthropicSSE 验证流式路径产出的 Anthropic SSE：
// 必须含 message_start / content_block_delta / message_delta / message_stop，且
// 上游的 text 与 thought 分别落到 text_delta 与 thinking_delta。
func TestCloudCodeStreamTranslatesToAnthropicSSE(t *testing.T) {
	server := cloudCodeFakeServer(t, "proj", []string{
		`{"response":{"candidates":[{"content":{"parts":[{"text":"think...","thought":true}]}}]}}`,
		`{"response":{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}}`,
		`{"response":{"candidates":[{"content":{"parts":[{"text":" world"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}`,
	}, nil, nil)
	defer server.Close()

	p := cloudCodeProviderForTest(t, server.URL, "cloudcode/gemini-3.8-flash", "tok")

	var sb strings.Builder
	req := mustRequest(t, `{"model":"cloudcode/gemini-3.8-flash","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	if err := p.ForwardStream(context.Background(), req, &sb); err != nil {
		t.Fatalf("ForwardStream failed: %v", err)
	}
	out := sb.String()

	for _, want := range []string{`"type":"message_start"`, `"type":"content_block_delta"`, `"type":"message_delta"`, `"type":"message_stop"`, `thinking_delta`, `text_delta`} {
		if !strings.Contains(out, want) {
			t.Fatalf("Anthropic SSE 缺少 %s，实际输出:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, " world") {
		t.Fatalf("文本增量未透传:\n%s", out)
	}
}

// TestCloudCodeUnavailableWhenLoggedOut 验证未登录时 Available=false，且不发请求。
func TestCloudCodeUnavailableWhenLoggedOut(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewCloudCodeProvider(&Config{Name: "cloudcode/gemini-3.8-flash", URL: server.URL, Auth: NewStaticAuth("")})
	if p.Available() {
		t.Fatalf("未登录时 Available 应为 false")
	}
	req := mustRequest(t, `{"model":"cloudcode/gemini-3.8-flash","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err == nil {
		t.Fatalf("未登录时应直接报错")
	}
	if hits != 0 {
		t.Fatalf("未登录时不应触达上游，实际 %d 次", hits)
	}
}

// TestCloudCodeSchemaSanitization 验证工具 schema 清洗：白名单外的键被剔除，
// type 数组归一化。上游对未知键报错，这是必要的兼容层。
func TestCloudCodeSchemaSanitization(t *testing.T) {
	cleaned := sanitizeCloudCodeSchema(map[string]any{
		"type":                 "object",
		"description":          "d",
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"additionalProperties": false,
		"properties": map[string]any{
			"name": map[string]any{"type": []any{"string", "null"}, "minLength": 1},
		},
		"required": []any{"name"},
	}).(map[string]any)

	if _, ok := cleaned["$schema"]; ok {
		t.Fatalf("$schema 应被剔除: %v", cleaned)
	}
	if _, ok := cleaned["additionalProperties"]; ok {
		t.Fatalf("additionalProperties 应被剔除: %v", cleaned)
	}
	props, _ := cleaned["properties"].(map[string]any)
	name, _ := props["name"].(map[string]any)
	if name["type"] != "string" {
		t.Fatalf("type 数组应归一化为 string，得到 %v", name["type"])
	}
	if name["nullable"] != true {
		t.Fatalf("含 null 的 type 数组应置 nullable=true，得到 %v", name["nullable"])
	}
}

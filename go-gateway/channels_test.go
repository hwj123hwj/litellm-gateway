package main

import (
	"testing"

	"github.com/weijian/go-llm-gateway/internal/channel"
)

// 这组测试直接断言 channels.go 的装配开关（buildChannelProvider），而不是复刻一份
// 配置：只有走真实装配，channels.go 里改错（例如漏掉 minimax 的 UseBearer）才会
// 让测试失败。集成链路的其余部分（鉴权头、请求头、401 续期）由
// internal/channel/provider_integration_test.go 覆盖。

func testChannelInstances(t *testing.T) channelInstances {
	t.Helper()
	dir := t.TempDir()
	return channelInstances{
		minimax:   channel.NewMiniMaxChannel(dir),
		cline:     channel.NewClineChannel(dir),
		lobster:   channel.NewLobsterChannel(dir),
		gemini:    channel.NewGeminiChannel(dir),
		codebuddy: channel.NewCodeBuddyChannel(dir),
		workbuddy: channel.NewWorkBuddyChannel(dir),
	}
}

// TestBuildChannelProviderMiniMaxUsesBearer 是回归测试：minimax 上游要求
// Authorization: Bearer，AnthropicProvider 的 UseBearer 必须为 true；
// 漏设时 provider 会发 x-api-key，对真实上游必然 401。
func TestBuildChannelProviderMiniMaxUsesBearer(t *testing.T) {
	instances := testChannelInstances(t)

	p, ok := buildChannelProvider("minimax", "MiniMax-M3", instances)
	if !ok {
		t.Fatalf("minimax 渠道应被识别")
	}
	if !p.UseBearer() {
		t.Fatalf("minimax provider 的 UseBearer 必须为 true，否则会发 x-api-key")
	}
	if p.Name() != "MiniMax-M3" {
		t.Fatalf("provider 名应为绑定的模型 ID，得到 %q", p.Name())
	}
	if p.URL() != instances.minimax.MessagesURL() {
		t.Fatalf("provider URL 应指向 minimax 推理端点，得到 %q", p.URL())
	}
	if p.APIKey() != "" {
		t.Fatalf("渠道 provider 不应带静态 api_key，得到 %q", p.APIKey())
	}
}

// TestBuildChannelProviderClineUsesBearer 验证 cline 走 OpenAI 协议（恒 Bearer）。
func TestBuildChannelProviderClineUsesBearer(t *testing.T) {
	instances := testChannelInstances(t)

	p, ok := buildChannelProvider("cline", "cline-free/mimo-v2.6-flash", instances)
	if !ok {
		t.Fatalf("cline 渠道应被识别")
	}
	if !p.UseBearer() {
		t.Fatalf("cline provider 必须用 Bearer")
	}
	if p.URL() != instances.cline.ChatURL() {
		t.Fatalf("provider URL 应指向 cline chat 端点，得到 %q", p.URL())
	}
}

// TestBuildChannelProviderLobsterUsesOpenAIEndpoint 验证 lobster 复用 OpenAI 协议：
// provider 必须指向 chat 端点、用 Bearer，且未登录时报告不可用（路由层据此跳过）。
func TestBuildChannelProviderLobsterUsesOpenAIEndpoint(t *testing.T) {
	instances := testChannelInstances(t)

	p, ok := buildChannelProvider("lobster", "kimi-k2.7-code", instances)
	if !ok {
		t.Fatalf("lobster 渠道应被识别")
	}
	if !p.UseBearer() {
		t.Fatalf("lobster provider 必须用 Bearer")
	}
	if p.URL() != instances.lobster.ChatURL() {
		t.Fatalf("provider URL 应指向 lobster chat 端点，得到 %q", p.URL())
	}
	if p.APIKey() != "" {
		t.Fatalf("渠道 provider 不应带静态 api_key，得到 %q", p.APIKey())
	}
	if available, ok := p.(interface{ Available() bool }); ok && available.Available() {
		t.Fatalf("未登录时 lobster provider 不应报告可用")
	}
}

// TestBuildChannelProviderGeminiUsesCloudCode 验证 gemini 走 CloudCode 私有协议：
// URL 指向 v1internal 流式端点、UseBearer，且未登录时报告不可用。
func TestBuildChannelProviderGeminiUsesCloudCode(t *testing.T) {
	instances := testChannelInstances(t)

	p, ok := buildChannelProvider("gemini", "cloudcode/gemini-3.8-flash-high", instances)
	if !ok {
		t.Fatalf("gemini 渠道应被识别")
	}
	if !p.UseBearer() {
		t.Fatalf("gemini provider 必须用 Bearer")
	}
	if want := "https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"; p.URL() != want {
		t.Fatalf("gemini provider URL 应为 CloudCode 流式端点，得到 %q", p.URL())
	}
	if available, ok := p.(interface{ Available() bool }); ok && available.Available() {
		t.Fatalf("未登录时 gemini provider 不应报告可用")
	}
}

// TestBuildChannelProviderCodeBuddyUsesOpenAIEndpoint 验证 codebuddy 复用 OpenAI
// 协议：URL 指向 /v2/chat/completions、Bearer，且未登录时报告不可用。
func TestBuildChannelProviderCodeBuddyUsesOpenAIEndpoint(t *testing.T) {
	instances := testChannelInstances(t)

	p, ok := buildChannelProvider("codebuddy", "codebuddy/glm-5.3", instances)
	if !ok {
		t.Fatalf("codebuddy 渠道应被识别")
	}
	if !p.UseBearer() {
		t.Fatalf("codebuddy provider 必须用 Bearer")
	}
	if p.URL() != instances.codebuddy.ChatURL() {
		t.Fatalf("provider URL 应指向 buddy chat 端点，得到 %q", p.URL())
	}
	if p.Name() != "codebuddy/glm-5.3" {
		t.Fatalf("provider 名应为绑定的模型 ID，得到 %q", p.Name())
	}
	if p.APIKey() != "" {
		t.Fatalf("渠道 provider 不应带静态 api_key，得到 %q", p.APIKey())
	}
	if available, ok := p.(interface{ Available() bool }); ok && available.Available() {
		t.Fatalf("未登录时 codebuddy provider 不应报告可用")
	}
}

// TestBuildChannelProviderWorkBuddyUsesItsOwnProduct 是回归测试：两个 buddy 渠道
// 共用一份实现，装配时若把 workbuddy 也指向 codebuddy 实例，端点会错到腾讯域上。
func TestBuildChannelProviderWorkBuddyUsesItsOwnProduct(t *testing.T) {
	instances := testChannelInstances(t)

	p, ok := buildChannelProvider("workbuddy", "workbuddy/gpt-5.6-sol", instances)
	if !ok {
		t.Fatalf("workbuddy 渠道应被识别")
	}
	if p.URL() != instances.workbuddy.ChatURL() {
		t.Fatalf("workbuddy provider URL 应指向 WorkBuddy 端点，得到 %q", p.URL())
	}
	if p.URL() == instances.codebuddy.ChatURL() {
		t.Fatalf("workbuddy 不应复用 codebuddy 的端点")
	}
	if available, ok := p.(interface{ Available() bool }); ok && available.Available() {
		t.Fatalf("未登录时 workbuddy provider 不应报告可用")
	}
}

// TestBuildChannelProviderUnknownChannel 验证未配置的渠道名不会给出 provider，
// 让 config_loader 跳过而不是注册一个空壳。
func TestBuildChannelProviderUnknownChannel(t *testing.T) {
	instances := testChannelInstances(t)

	if p, ok := buildChannelProvider("qoder", "whatever", instances); ok || p != nil {
		t.Fatalf("未实现的渠道不应返回 provider，得到 %v ok=%v", p, ok)
	}
}

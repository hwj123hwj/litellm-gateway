package main

import (
	"testing"

	"github.com/weijian/go-llm-gateway/internal/channel"
)

// 这组测试直接断言 channels.go 的装配开关（buildChannelProvider），而不是复刻一份
// 配置：只有走真实装配，channels.go 里改错（例如漏掉 minimax 的 UseBearer）才会
// 让测试失败。集成链路的其余部分（鉴权头、请求头、401 续期）由
// internal/channel/provider_integration_test.go 覆盖。

// TestBuildChannelProviderMiniMaxUsesBearer 是回归测试：minimax 上游要求
// Authorization: Bearer，AnthropicProvider 的 UseBearer 必须为 true；
// 漏设时 provider 会发 x-api-key，对真实上游必然 401。
func TestBuildChannelProviderMiniMaxUsesBearer(t *testing.T) {
	dir := t.TempDir()
	minimax := channel.NewMiniMaxChannel(dir)
	cline := channel.NewClineChannel(dir)

	p, ok := buildChannelProvider("minimax", "MiniMax-M3", minimax, cline)
	if !ok {
		t.Fatalf("minimax 渠道应被识别")
	}
	if !p.UseBearer() {
		t.Fatalf("minimax provider 的 UseBearer 必须为 true，否则会发 x-api-key")
	}
	if p.Name() != "MiniMax-M3" {
		t.Fatalf("provider 名应为绑定的模型 ID，得到 %q", p.Name())
	}
	if p.URL() != minimax.MessagesURL() {
		t.Fatalf("provider URL 应指向 minimax 推理端点，得到 %q", p.URL())
	}
	if p.APIKey() != "" {
		t.Fatalf("渠道 provider 不应带静态 api_key，得到 %q", p.APIKey())
	}
}

// TestBuildChannelProviderClineUsesBearer 验证 cline 走 OpenAI 协议（恒 Bearer）。
func TestBuildChannelProviderClineUsesBearer(t *testing.T) {
	dir := t.TempDir()
	minimax := channel.NewMiniMaxChannel(dir)
	cline := channel.NewClineChannel(dir)

	p, ok := buildChannelProvider("cline", "cline-free/mimo-v2.6-flash", minimax, cline)
	if !ok {
		t.Fatalf("cline 渠道应被识别")
	}
	if !p.UseBearer() {
		t.Fatalf("cline provider 必须用 Bearer")
	}
	if p.URL() != cline.ChatURL() {
		t.Fatalf("provider URL 应指向 cline chat 端点，得到 %q", p.URL())
	}
}

// TestBuildChannelProviderUnknownChannel 验证未配置的渠道名不会给出 provider，
// 让 config_loader 跳过而不是注册一个空壳。
func TestBuildChannelProviderUnknownChannel(t *testing.T) {
	dir := t.TempDir()
	minimax := channel.NewMiniMaxChannel(dir)
	cline := channel.NewClineChannel(dir)

	if p, ok := buildChannelProvider("qoder", "whatever", minimax, cline); ok || p != nil {
		t.Fatalf("未实现的渠道不应返回 provider，得到 %v ok=%v", p, ok)
	}
}

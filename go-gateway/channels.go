package main

import (
	"log"
	"path/filepath"

	"github.com/weijian/go-llm-gateway/internal/channel"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

// 账号渠道（第二类）装配。
//
// 与普通 provider 的差别在于：凭据来自交互式登录（网关内置设备码流程），会过期、
// 需要续期，因此不能写进 providers.yaml 的 api_key_env。这里把「渠道实例」与
// 「由它派生的 provider」分开：
//   - channel.Registry 汇总所有渠道，Admin API 通过它发起登录、查看状态、登出；
//   - provider 侧只拿到每个渠道的账号池（作为 AuthSource）与一个绑定模型的
//     provider 实例，模型清单仍在 providers.yaml 里声明（type: channel）。
//
// 两者在 provider 包与 channel 包之间不产生 import 依赖：provider 只认 AuthSource
// 接口，channel.Accounts 结构化满足它。
func setupChannelProviders(router *provider.Router, registry *channel.Registry, logger *log.Logger) {
	dir := filepath.Join(defaultGatewayHome(), "channels")

	minimax := channel.NewMiniMaxChannel(dir)
	registry.Register(minimax)
	logger.Printf("Account channel registered: minimax (credentials: %s)", filepath.Join(dir, "minimax.json"))

	cline := channel.NewClineChannel(dir)
	registry.Register(cline)
	logger.Printf("Account channel registered: cline (credentials: %s)", filepath.Join(dir, "cline.json"))

	// 工厂按「渠道名 + 模型 ID」构造绑定模型的 provider。渠道实例与账号池在这里
	// 复用同一份，保证登录写回的令牌立即对 provider 生效。
	provider.SetChannelProviderFactory(func(channelName, modelID string) (provider.Provider, bool) {
		return buildChannelProvider(channelName, modelID, minimax, cline)
	})

	for _, name := range registry.Names() {
		c, _ := registry.Get(name)
		logger.Printf("Account channel %s: %d account(s) loaded", name, c.Accounts().Len())
	}
}

// buildChannelProvider 是渠道 → provider 的装配开关，独立成函数以便测试直接断言
// 装配结果（UseBearer、协议类型、请求头），不需要起完整的网关。
func buildChannelProvider(channelName, modelID string, minimax *channel.MiniMaxChannel, cline *channel.ClineChannel) (provider.Provider, bool) {
	switch channelName {
	case "minimax":
		// 上游是 Anthropic Messages 协议：复用 AnthropicProvider 的传输与
		// SSE 透传，仅补一个请求体改写（M3.1 系列强制 adaptive 思考）。
		// UseBearer 必须为 true：该端点鉴权是 Authorization: Bearer，
		// 不是 Anthropic 官方的 x-api-key（AnthropicProvider 默认走后者）。
		return provider.NewAnthropicProvider(&provider.Config{
			Name:      modelID,
			URL:       minimax.MessagesURL(),
			Auth:      minimax.Accounts(),
			UseBearer: true,
			Transform: channel.MiniMaxTransformRequest,
		}), true
	case "cline":
		// 上游是 OpenAI Chat Completions 协议：复用 OpenAIProvider 的协议转换，
		// 补上复刻官方客户端所需的固定请求头（含 workos: 令牌前缀）。
		return provider.NewOpenAIProvider(&provider.Config{
			Name:         modelID,
			URL:          cline.ChatURL(),
			Auth:         cline.Accounts(),
			ExtraHeaders: channel.ClineClientHeaders(),
		}), true
	}
	return nil, false
}

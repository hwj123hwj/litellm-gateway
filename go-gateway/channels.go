package main

import (
	"log"
	"path/filepath"

	"github.com/weijian/go-llm-gateway/internal/channel"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

// 账号渠道（第二类）装配。
//
// 与普通 provider 的差别在于：凭据来自交互式登录（网关内置登录流程），会过期、
// 需要续期，因此不能写进 providers.yaml 的 api_key_env。这里把「渠道实例」与
// 「由它派生的 provider」分开：
//   - channel.Registry 汇总所有渠道，Admin API 通过它发起登录、查看状态、登出；
//   - provider 侧只拿到每个渠道的账号池（作为 AuthSource）与一个绑定模型的
//     provider 实例，模型清单仍在 providers.yaml 里声明（type: channel）。
//
// 两者在 provider 包与 channel 包之间不产生 import 依赖：provider 只认 AuthSource
// 接口，channel.Accounts 结构化满足它。
//
// 登录方式分两类，但对 Admin API 暴露成一回事：minimax/cline 是设备码轮询，
// lobster/gemini 是授权码 + 本机回环回调（见 channel.callbackServer）。回调登录
// 把登录地址放进 DeviceCode.VerificationURIComplete，Admin API 原样回成 login_url。
func setupChannelProviders(router *provider.Router, registry *channel.Registry, logger *log.Logger) {
	dir := filepath.Join(defaultGatewayHome(), "channels")

	instances := channelInstances{
		minimax: channel.NewMiniMaxChannel(dir),
		cline:   channel.NewClineChannel(dir),
		lobster: channel.NewLobsterChannel(dir),
		gemini:  channel.NewGeminiChannel(dir),
	}

	for _, c := range []channel.Channel{instances.minimax, instances.cline, instances.lobster, instances.gemini} {
		registry.Register(c)
		logger.Printf("Account channel registered: %s (credentials: %s)", c.Name(), filepath.Join(dir, c.Name()+".json"))
	}

	// 工厂按「渠道名 + 模型 ID」构造绑定模型的 provider。渠道实例与账号池在这里
	// 复用同一份，保证登录写回的令牌立即对 provider 生效。
	provider.SetChannelProviderFactory(func(channelName, modelID string) (provider.Provider, bool) {
		return buildChannelProvider(channelName, modelID, instances)
	})

	for _, name := range registry.Names() {
		c, _ := registry.Get(name)
		logger.Printf("Account channel %s: %d account(s) loaded", name, c.Accounts().Len())
	}
}

// channelInstances 汇总已装配的渠道实例，按渠道名分发给 buildChannelProvider。
// 收敛成一个结构体而不是逐个传参：新增渠道时只改这里与 switch，不必再改所有
// 调用点的签名。
type channelInstances struct {
	minimax *channel.MiniMaxChannel
	cline   *channel.ClineChannel
	lobster *channel.LobsterChannel
	gemini  *channel.GeminiChannel
}

// buildChannelProvider 是渠道 → provider 的装配开关，独立成函数以便测试直接断言
// 装配结果（UseBearer、协议类型、请求头），不需要起完整的网关。
func buildChannelProvider(channelName, modelID string, ch channelInstances) (provider.Provider, bool) {
	switch channelName {
	case "minimax":
		// 上游是 Anthropic Messages 协议：复用 AnthropicProvider 的传输与
		// SSE 透传，仅补一个请求体改写（M3.1 系列强制 adaptive 思考）。
		// UseBearer 必须为 true：该端点鉴权是 Authorization: Bearer，
		// 不是 Anthropic 官方的 x-api-key（AnthropicProvider 默认走后者）。
		return provider.NewAnthropicProvider(&provider.Config{
			Name:      modelID,
			URL:       ch.minimax.MessagesURL(),
			Auth:      ch.minimax.Accounts(),
			UseBearer: true,
			Transform: channel.MiniMaxTransformRequest,
		}), true
	case "cline":
		// 上游是 OpenAI Chat Completions 协议：复用 OpenAIProvider 的协议转换，
		// 补上复刻官方客户端所需的固定请求头（含 workos: 令牌前缀）。
		return provider.NewOpenAIProvider(&provider.Config{
			Name:         modelID,
			URL:          ch.cline.ChatURL(),
			Auth:         ch.cline.Accounts(),
			ExtraHeaders: channel.ClineClientHeaders(),
		}), true
	case "lobster":
		// 上游同样是 OpenAI Chat Completions 协议（/api/proxy/v1/chat/completions），
		// 复用 OpenAIProvider；渠道层提供令牌、复刻头与续期。上游强制 stream:true，
		// 走 provider 统一的流式路径。
		return provider.NewOpenAIProvider(&provider.Config{
			Name:         modelID,
			URL:          ch.lobster.ChatURL(),
			Auth:         ch.lobster.Accounts(),
			ExtraHeaders: ch.lobster.ClientHeaders(),
		}), true
	case "gemini":
		// 上游是 CloudCode 私有协议（v1internal:streamGenerateContent），既不是
		// OpenAI 也不是 Anthropic：用专门的 CloudCodeProvider 做信封与 SSE 翻译。
		// Auth 用渠道的 AuthSource（账号池 + CloudCode 项目缓存）。
		return provider.NewCloudCodeProvider(&provider.Config{
			Name: modelID,
			Auth: ch.gemini.AuthSource(),
		}), true
	}
	return nil, false
}

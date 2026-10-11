package channel

// LoginInfo describes prerequisites without exposing application credentials.
type LoginInfo struct {
	Available         bool   `json:"login_available"`
	NeedsClientConfig bool   `json:"needs_client_config"`
	LocalOnly         bool   `json:"local_login_only"`
	SetupHint         string `json:"setup_hint,omitempty"`
	Anonymous         bool   `json:"anonymous_available"`
}

func LoginInfoFor(ch Channel) LoginInfo {
	info := LoginInfo{Available: true}
	switch c := ch.(type) {
	case *GeminiChannel:
		info.NeedsClientConfig = true
		info.Available = c.configured()
		info.SetupHint = "在网关主机配置 GEMINI_CHANNEL_CLIENT_ID / GEMINI_CHANNEL_CLIENT_SECRET，或 gemini-client.json；然后重启网关。"
	case *TraeChannel:
		info.NeedsClientConfig = true
		info.Available = c.configured()
		info.SetupHint = "可自动读取网关主机已安装的 TRAE CN 客户端配置；或配置 TRAE_CHANNEL_CLIENT_ID / TRAE_CHANNEL_APP_ID、trae-client.json，然后重启网关。"
		if c.localClient != nil {
			info.NeedsClientConfig = false
			info.SetupHint = "已识别本机 TRAE CN 客户端配置，点击登录完成浏览器授权。"
		}
	case *LoomyChannel:
		info.NeedsClientConfig = true
		info.Available = c.configured()
		info.SetupHint = "在网关主机配置 LOOMY_ACCESS_KEY_ID / LOOMY_ACCESS_KEY_SECRET，或 loomy-client.json；然后重启网关。"
	}
	switch ch.Name() {
	case "lobster", "gemini", "trae", "loomy", "raccoon":
		info.LocalOnly = true
	case "opencode":
		info.Anonymous = true
	}
	return info
}

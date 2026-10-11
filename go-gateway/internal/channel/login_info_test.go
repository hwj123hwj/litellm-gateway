package channel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLoginInfoReportsMissingConfigurationWithoutCredentials(t *testing.T) {
	for _, name := range []string{"GEMINI_CHANNEL_CLIENT_ID", "GEMINI_CHANNEL_CLIENT_SECRET", "CMDC_PAK_GOOGLE_CLIENT_ID", "CMDC_PAK_GOOGLE_CLIENT_SECRET", "TRAE_CHANNEL_CLIENT_ID", "TRAE_CHANNEL_APP_ID", "LOOMY_ACCESS_KEY_ID", "LOOMY_ACCESS_KEY_SECRET"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	t.Setenv("TRAE_CHANNEL_CLIENT_PRODUCT", dir+"/not-installed.json")
	for _, ch := range []Channel{NewGeminiChannel(dir), NewTraeChannel(dir), NewLoomyChannel(dir)} {
		info := LoginInfoFor(ch)
		if info.Available || !info.NeedsClientConfig || !info.LocalOnly || info.SetupHint == "" {
			t.Fatalf("%s %+v", ch.Name(), info)
		}
	}
	t.Setenv("GEMINI_CHANNEL_CLIENT_ID", "client-id-sentinel")
	t.Setenv("GEMINI_CHANNEL_CLIENT_SECRET", "client-secret-sentinel")
	info := LoginInfoFor(NewGeminiChannel(dir))
	raw, _ := json.Marshal(info)
	if !info.Available || strings.Contains(string(raw), "sentinel") {
		t.Fatal(string(raw))
	}
	for _, ch := range []Channel{NewMiniMaxChannel(dir), NewClineChannel(dir), NewLobsterChannel(dir), NewCodeBuddyChannel(dir), NewWorkBuddyChannel(dir), NewRaccoonChannel(dir), NewOpenCodeChannel(dir)} {
		info := LoginInfoFor(ch)
		if !info.Available || info.NeedsClientConfig {
			t.Fatalf("%s %+v", ch.Name(), info)
		}
	}
	if !LoginInfoFor(NewOpenCodeChannel(dir)).Anonymous {
		t.Fatal("OpenCode anonymous capability missing")
	}
}

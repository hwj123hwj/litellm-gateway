package channel

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const traeProductFixture = `{"providerCode":"cn","packageType":"TRAE_CN","quality":"stable","appVersion":"3.3.104","tronBuildVersion":"2.3.test","iCubeApp":{"authConfig":{"TRAE":{"stable":"local-client-sentinel"},"SOLO":{"stable":"solo-client-sentinel"}}},"bootConfig":{"agent":{"appId":"local-app-sentinel"}}}`

func localTraeFixture(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "product.json")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstalledTraeConfigurationEnablesNativeLogin(t *testing.T) {
	clearTraeCredentialEnv(t)
	t.Setenv("TRAE_CHANNEL_CLIENT_PRODUCT", localTraeFixture(t, traeProductFixture))
	c := NewTraeChannel(t.TempDir())
	if !c.configured() || c.localClient == nil || c.clientID != "local-client-sentinel" || c.appID != "local-app-sentinel" {
		t.Fatal("installed configuration not used")
	}
	info := LoginInfoFor(c)
	raw, _ := json.Marshal(info)
	if !info.Available || info.NeedsClientConfig || strings.Contains(string(raw), "sentinel") {
		t.Fatal("incorrect or unsafe login metadata")
	}
	c.callbackPort = 0
	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(session.DeviceCode().LoginURL())
	query := u.Query()
	if query.Get("auth_from") != "trae" || query.Get("client_id") != c.clientID || query.Get("x_app_version") != "3.3.104" || query.Get("plugin_version") != "2.3.test" {
		t.Fatal("wrong installed-client authorization parameters")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { session.Wait(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("installed-client login did not cancel")
	}
}

func TestTraeManualConfigurationKeepsPriority(t *testing.T) {
	clearTraeCredentialEnv(t)
	t.Setenv("TRAE_CHANNEL_CLIENT_PRODUCT", localTraeFixture(t, traeProductFixture))
	dir := filepath.Join(t.TempDir(), "channels")
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), traeClientCredentialFile), []byte(`{"client_id":"file-client","app_id":"file-app"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewTraeChannel(dir)
	if c.clientID != "file-client" || c.appID != "file-app" || c.localClient != nil {
		t.Fatal("local installation overrode explicit file")
	}
	t.Setenv("TRAE_CHANNEL_CLIENT_ID", "env-client")
	t.Setenv("TRAE_CHANNEL_APP_ID", "env-app")
	c = NewTraeChannel(dir)
	if c.clientID != "env-client" || c.appID != "env-app" || c.localClient != nil {
		t.Fatal("environment priority lost")
	}
}

func TestTraeLocalProductRejectsUnsupportedOrIncompleteInstallations(t *testing.T) {
	for _, contents := range []string{"{", strings.Replace(traeProductFixture, `"cn"`, `"us"`, 1), strings.Replace(traeProductFixture, `"TRAE_CN"`, `"UNKNOWN"`, 1), strings.Replace(traeProductFixture, `"local-app-sentinel"`, `""`, 1)} {
		if readTraeLocalClient(localTraeFixture(t, contents)) != nil {
			t.Fatal("unsupported installation accepted")
		}
	}
	solo := readTraeLocalClient(localTraeFixture(t, strings.Replace(traeProductFixture, `"TRAE_CN"`, `"SOLO_CN"`, 1)))
	if solo == nil || solo.AuthFrom != "solo" || solo.ClientID != "solo-client-sentinel" {
		t.Fatal("SOLO OAuth client not distinguished")
	}
}

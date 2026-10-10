package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseMiniMaxTokenFull(t *testing.T) {
	exp := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	access := makeJWT(t, map[string]any{"sub": "user-123", "exp": float64(exp.Unix())})

	cred, err := parseMiniMaxToken(map[string]any{
		"access_token":  access,
		"refresh_token": "refresh-abc",
		"token_type":    "Bearer",
		"scope":         "agent.default openid",
		"expires_in":    float64(7200),
	}, "")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cred.AccessToken != access {
		t.Fatalf("access token 未原样保留")
	}
	if cred.RefreshToken != "refresh-abc" {
		t.Fatalf("refresh token 错误: %s", cred.RefreshToken)
	}
	if cred.AccountID != "user-123" {
		t.Fatalf("应从 JWT sub 解析账号: %s", cred.AccountID)
	}
	if !cred.ExpiresAt.Equal(exp) {
		t.Fatalf("过期时间应优先取 JWT exp：want %v got %v", exp, cred.ExpiresAt)
	}
}

func TestParseMiniMaxTokenFallsBackToExpiresIn(t *testing.T) {
	// 非 JWT 的 access token：无法解 exp，应回退到 expires_in（相对现在）。
	before := time.Now()
	cred, err := parseMiniMaxToken(map[string]any{
		"access_token":  "opaque-token",
		"refresh_token": "r",
		"token_type":    "Bearer",
		"expires_in":    float64(3600),
	}, "")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cred.AccountID != "" {
		t.Fatalf("非 JWT 不应解析出账号，得到 %s", cred.AccountID)
	}
	if cred.ExpiresAt.Before(before.Add(3500 * time.Second)) {
		t.Fatalf("expires_in 回退值不合理: %v", cred.ExpiresAt)
	}
}

func TestParseMiniMaxTokenKeepsPreviousRefresh(t *testing.T) {
	// 续期响应不回传 refresh_token 时必须沿用旧值，否则下次续期会失败。
	access := makeJWT(t, map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())})
	cred, err := parseMiniMaxToken(map[string]any{
		"access_token": access,
		"token_type":   "Bearer",
	}, "old-refresh")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cred.RefreshToken != "old-refresh" {
		t.Fatalf("应沿用旧 refresh token，得到 %s", cred.RefreshToken)
	}
}

func TestParseMiniMaxTokenRejects(t *testing.T) {
	cases := map[string]map[string]any{
		"缺 access_token":      {"refresh_token": "r", "token_type": "Bearer"},
		"缺 refresh_token":     {"access_token": "a", "token_type": "Bearer"},
		"token_type 非 Bearer": {"access_token": "a", "refresh_token": "r", "token_type": "mac"},
		"scope 不含 agent.default": {
			"access_token": "a", "refresh_token": "r", "token_type": "Bearer", "scope": "other.scope",
		},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseMiniMaxToken(body, ""); err == nil {
				t.Fatalf("期望解析失败，实际通过")
			}
		})
	}
}

func TestMiniMaxTransformRequestForcesAdaptiveForM31(t *testing.T) {
	in := []byte(`{"model":"MiniMax-M3.1-Flash-Preview","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"disabled"}}`)
	out, err := MiniMaxTransformRequest(in)
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("output not json: %v", err)
	}
	if string(payload["thinking"]) != `{"type":"adaptive"}` {
		t.Fatalf("M3.1 应强制 adaptive，得到 %s", payload["thinking"])
	}
	// 非 thinking 字段必须原样保留。
	if !strings.Contains(string(out), "messages") {
		t.Fatalf("transform 丢掉了其他字段: %s", out)
	}
}

func TestMiniMaxTransformRequestPassthroughForOtherModels(t *testing.T) {
	in := []byte(`{"model":"MiniMax-M3","thinking":{"type":"disabled"}}`)
	out, err := MiniMaxTransformRequest(in)
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	// 非 M3.1 系列应原样返回同一个切片，不做任何改写。
	if string(out) != string(in) {
		t.Fatalf("非目标模型不应改写，want %s got %s", in, out)
	}
}

func TestMiniMaxTransformRequestInvalidJSONPassthrough(t *testing.T) {
	in := []byte(`not-json`)
	out, err := MiniMaxTransformRequest(in)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if string(out) != string(in) {
		t.Fatalf("非法 JSON 应原样透传，得到 %s", out)
	}
}

// TestMiniMaxDeviceLoginFlow 用假上游走一遍完整设备码登录：申请设备码 → 第一次
// 轮询 pending → 第二次成功 → 凭据落盘并进内存池。
func TestMiniMaxDeviceLoginFlow(t *testing.T) {
	pollCount := 0
	access := makeJWT(t, map[string]any{"sub": "acct-1", "exp": float64(time.Now().Add(2 * time.Hour).Unix())})

	mux := http.NewServeMux()
	mux.HandleFunc(miniMaxDeviceCodePath, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("client_id") != miniMaxClientID {
			t.Errorf("client_id 错误: %s", r.Form.Get("client_id"))
		}
		if r.Form.Get("code_challenge_method") != "S256" {
			t.Errorf("必须使用 PKCE S256")
		}
		writeJSON(w, map[string]any{
			"device_code":      "dev-1",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://account.minimax.cn/device",
			"expires_in":       300,
			"interval":         1,
		})
	})
	mux.HandleFunc(miniMaxTokenPath, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("grant_type 错误: %s", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code_verifier") == "" {
			t.Errorf("轮询必须带 code_verifier")
		}
		pollCount++
		if pollCount == 1 {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"error": "authorization_pending"})
			return
		}
		writeJSON(w, map[string]any{
			"access_token":  access,
			"refresh_token": "refresh-1",
			"token_type":    "Bearer",
			"scope":         miniMaxScope,
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ch := NewMiniMaxChannel(t.TempDir())
	ch.accountHost = server.URL
	ch.apiHost = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := ch.BeginLogin(ctx)
	if err != nil {
		t.Fatalf("BeginLogin failed: %v", err)
	}
	dc := session.DeviceCode()
	if dc.UserCode != "ABCD-EFGH" || dc.Interval != time.Second {
		t.Fatalf("设备码展示信息错误: %+v", dc)
	}

	cred, err := session.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if cred.AccessToken != access || cred.AccountID != "acct-1" {
		t.Fatalf("登录结果错误: %+v", cred)
	}
	if ch.Accounts().Len() != 1 || !ch.Accounts().LoggedIn() {
		t.Fatalf("凭据未进入账号池")
	}
	if got := ch.Accounts().BearerToken(); got != access {
		t.Fatalf("账号池令牌不一致: %s", got)
	}
	if got := ch.MessagesURL(); got != server.URL+miniMaxInferPath {
		t.Fatalf("MessagesURL 应指向可覆盖的 host: %s", got)
	}
}

// TestMiniMaxDeviceLoginPersists 验证登录产物真的落盘，重开渠道仍能读到。
func TestMiniMaxDeviceLoginPersists(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	writeCreds(t, store, miniMaxChannel, Credential{AccessToken: "persisted", RefreshToken: "r"})

	reopened := NewMiniMaxChannel(dir)
	if !reopened.Accounts().LoggedIn() {
		t.Fatalf("重开渠道后应能读到已落盘凭据")
	}
	if got := reopened.Accounts().BearerToken(); got != "persisted" {
		t.Fatalf("读到的令牌错误: %s", got)
	}
}

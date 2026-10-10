package channel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestParseClineTokenEnvelopeCamelCase(t *testing.T) {
	expiry := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	body := map[string]any{
		"data": map[string]any{
			"accessToken":  "cl-access",
			"refreshToken": "cl-refresh",
			"expiresAt":    float64(expiry.UnixMilli()),
			"userInfo": map[string]any{
				"clineUserId": "cline-user-9",
				"email":       "a@b.com",
			},
		},
	}
	cred, err := parseClineToken(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cred.AccessToken != clineTokenPrefix+"cl-access" {
		t.Fatalf("access token 应带 workos: 前缀，得到 %s", cred.AccessToken)
	}
	if cred.RefreshToken != "cl-refresh" {
		t.Fatalf("refresh token 错误: %s", cred.RefreshToken)
	}
	if cred.AccountID != "cline-user-9" {
		t.Fatalf("账号 id 错误: %s", cred.AccountID)
	}
	if cred.Nickname != "a@b.com" {
		t.Fatalf("昵称应回退到 email: %s", cred.Nickname)
	}
	if !cred.ExpiresAt.Equal(expiry) {
		t.Fatalf("毫秒时间戳解析错误: want %v got %v", expiry, cred.ExpiresAt)
	}
}

func TestParseClineTokenFlatSnakeCase(t *testing.T) {
	// 无 data 信封、下划线字段、秒级时间戳、无 email 但有姓名的场景。
	body := map[string]any{
		"access_token":  "flat-access",
		"refresh_token": "flat-refresh",
		"account_id":    "acct-flat",
		"expires_at":    float64(time.Now().Add(time.Hour).Unix()),
	}
	cred, err := parseClineToken(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cred.AccessToken != clineTokenPrefix+"flat-access" {
		t.Fatalf("前缀缺失: %s", cred.AccessToken)
	}
	if cred.AccountID != "acct-flat" {
		t.Fatalf("账号 id 错误: %s", cred.AccountID)
	}
	if cred.Nickname != "acct-flat" {
		t.Fatalf("无邮箱时昵称应回退到账号 id: %s", cred.Nickname)
	}
	if cred.Expired(time.Now()) {
		t.Fatalf("秒级时间戳解析后不应已过期: %v", cred.ExpiresAt)
	}
}

func TestParseClineTokenMissingAccess(t *testing.T) {
	if _, err := parseClineToken(map[string]any{"data": map[string]any{}}); err == nil {
		t.Fatalf("缺访问令牌应报错")
	}
}

func TestParseClineTokenNicknameFromName(t *testing.T) {
	body := map[string]any{
		"accessToken": "t",
		"userInfo":    map[string]any{"firstName": "Ada", "lastName": "Lovelace"},
	}
	cred, err := parseClineToken(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cred.Nickname != "Ada Lovelace" {
		t.Fatalf("昵称应由姓名拼接: %q", cred.Nickname)
	}
}

func TestClineBearerValueIdempotent(t *testing.T) {
	if got := clineBearerValue("abc"); got != "workos:abc" {
		t.Fatalf("应加前缀，得到 %s", got)
	}
	if got := clineBearerValue("workos:abc"); got != "workos:abc" {
		t.Fatalf("已有前缀不应重复添加，得到 %s", got)
	}
	if got := clineBearerValue("  "); got != "" {
		t.Fatalf("空白应归一为空串，得到 %q", got)
	}
}

func TestParseClineTimestampForms(t *testing.T) {
	millis := time.Now().Truncate(time.Second)
	cases := []struct {
		name  string
		value any
		want  time.Time
	}{
		{"毫秒 float", float64(millis.UnixMilli()), millis},
		{"秒 float", float64(millis.Unix()), millis},
		{"json.Number 毫秒", json.Number(strconv.FormatInt(millis.UnixMilli(), 10)), millis},
		{"ISO 字符串", millis.Format(time.RFC3339), millis},
		{"数字字符串(秒)", strconv.FormatInt(millis.Unix(), 10), millis},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseClineTimestamp(tc.value)
			if !got.Equal(tc.want) {
				t.Fatalf("want %v got %v", tc.want, got)
			}
		})
	}

	for name, value := range map[string]any{
		"零值":  float64(0),
		"空串":  "",
		"乱码串": "not-a-time",
		"无关键": nil,
		"负毫秒": float64(-5),
	} {
		t.Run("非法/"+name, func(t *testing.T) {
			if got := parseClineTimestamp(value); !got.IsZero() {
				t.Fatalf("应返回零值，得到 %v", got)
			}
		})
	}
}

func formatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}

// TestClineDeviceLoginFlow 走完两跳：WorkOS 设备码 → pending → 成功 → register
// 换回 Cline 令牌 → 落盘。
func TestClineDeviceLoginFlow(t *testing.T) {
	pollCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc(clineDeviceCodeURL, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("client_id") != clineWorkOSClient {
			t.Errorf("WorkOS client_id 错误: %s", r.Form.Get("client_id"))
		}
		writeJSON(w, map[string]any{
			"device_code":      "wos-dev",
			"user_code":        "WXYZ-1234",
			"verification_uri": "https://workos.com/device",
			"expires_in":       300,
			"interval":         1,
		})
	})
	mux.HandleFunc(clineAuthenticate, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		pollCount++
		if pollCount == 1 {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"error": "authorization_pending"})
			return
		}
		writeJSON(w, map[string]any{
			"access_token":  "wos-access",
			"refresh_token": "wos-refresh",
		})
	})
	mux.HandleFunc(clineRegisterPath, func(w http.ResponseWriter, r *http.Request) {
		// register 必须带复刻客户端的固定头，否则上游按非官方客户端处理。
		for name, want := range ClineClientHeaders() {
			if got := r.Header.Get(name); got != want {
				t.Errorf("register 缺少头 %s=%s，得到 %q", name, want, got)
			}
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode register body: %v", err)
		}
		if payload["accessToken"] != "wos-access" || payload["refreshToken"] != "wos-refresh" {
			t.Errorf("register 未带上 WorkOS 令牌: %+v", payload)
		}
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"accessToken":  "cline-access",
				"refreshToken": "cline-refresh",
				"userInfo":     map[string]any{"clineUserId": "u-1", "email": "u@example.com"},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ch := NewClineChannel(t.TempDir())
	ch.apiHost = server.URL
	ch.workosHost = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := ch.BeginLogin(ctx)
	if err != nil {
		t.Fatalf("BeginLogin failed: %v", err)
	}
	if session.Channel() != clineChannel {
		t.Fatalf("channel 名错误: %s", session.Channel())
	}
	if session.DeviceCode().UserCode != "WXYZ-1234" {
		t.Fatalf("设备码展示信息错误: %+v", session.DeviceCode())
	}

	cred, err := session.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if cred.AccessToken != clineTokenPrefix+"cline-access" {
		t.Fatalf("入池令牌应带 workos: 前缀: %s", cred.AccessToken)
	}
	if cred.RefreshToken != "cline-refresh" || cred.AccountID != "u-1" {
		t.Fatalf("登录结果错误: %+v", cred)
	}
	if !ch.Accounts().LoggedIn() {
		t.Fatalf("凭据未进入账号池")
	}
	if got := ch.ChatURL(); got != server.URL+clineChatPath {
		t.Fatalf("ChatURL 应指向可覆盖 host: %s", got)
	}
}

// TestClineRefreshRejectedIsTerminal 验证 401 续期被拒被标成终态（需重新登录），
// 而不是普通错误。
func TestClineRefreshRejectedIsTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]any{"error": "invalid_grant"})
	}))
	defer server.Close()

	ch := NewClineChannel(t.TempDir())
	ch.apiHost = server.URL

	_, err := ch.refreshCredential(context.Background(), Credential{RefreshToken: "dead"})
	if err == nil {
		t.Fatalf("期望续期失败")
	}
	if !errors.Is(err, ErrLoginCancelled) {
		t.Fatalf("401 续期被拒应为终态错误，得到 %v", err)
	}
}

// TestClineRefreshSuccess 验证续期响应解析与前缀补全。
func TestClineRefreshSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["grantType"] != "refresh_token" {
			t.Errorf("续期 body 字段名应为驼峰 grantType: %+v", payload)
		}
		if payload["refreshToken"] != "old-refresh" {
			t.Errorf("续期未带上旧 refresh token: %+v", payload)
		}
		writeJSON(w, map[string]any{"access_token": "new-access", "refresh_token": "new-refresh"})
	}))
	defer server.Close()

	ch := NewClineChannel(t.TempDir())
	ch.apiHost = server.URL

	cred, err := ch.refreshCredential(context.Background(), Credential{RefreshToken: "old-refresh", AccountID: "keep"})
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if cred.AccessToken != clineTokenPrefix+"new-access" {
		t.Fatalf("续期令牌前缀错误: %s", cred.AccessToken)
	}
	if cred.RefreshToken != "new-refresh" {
		t.Fatalf("应更新 refresh token: %s", cred.RefreshToken)
	}
	if cred.AccountID != "keep" {
		t.Fatalf("上游未回传账号时应保留旧值: %s", cred.AccountID)
	}
}

func TestClineRefreshRequiresRefreshToken(t *testing.T) {
	ch := NewClineChannel(t.TempDir())
	if _, err := ch.refreshCredential(context.Background(), Credential{}); err == nil {
		t.Fatalf("无 refresh token 应报错")
	}
}

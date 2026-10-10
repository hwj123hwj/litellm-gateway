package channel

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

// makeJWT 拼一个只有 payload 有意义的 JWT（header/signature 用占位），用于测试
// exp / sub 声明解析。不做签名校验，解析方也不校验。
func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + body + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
}

// writeCreds 直接把凭据数组写进渠道存储，模拟「已登录」的既成状态。
func writeCreds(t *testing.T, store *Store, ch string, creds ...Credential) {
	t.Helper()
	if err := store.Save(ch, creds); err != nil {
		t.Fatalf("save creds: %v", err)
	}
}

// writeJSON 给假上游写一个 JSON 响应体。
func writeJSON(w http.ResponseWriter, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

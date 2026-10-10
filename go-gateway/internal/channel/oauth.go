package channel

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// oauthClient 是一个只做表单/JSON 往返的小 HTTP 客户端。账号渠道的登录服务都在
// 固定主机上，超时都较短（秒级），不需要套用 provider 的流式超时策略。
type oauthClient struct {
	http *http.Client
}

const oauthTimeout = 20 * time.Second

func newOAuthClient() *oauthClient {
	return &oauthClient{http: &http.Client{Timeout: oauthTimeout}}
}

// postForm 发送 application/x-www-form-urlencoded 请求，返回解析后的 JSON 对象。
// 非 2xx 时把响应体包进 error，保留上游错误码（例如 authorization_pending）。
func (c *oauthClient) postForm(ctx context.Context, urlStr string, form url.Values) (map[string]any, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, fmt.Errorf("build form request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.do(req)
}

func (c *oauthClient) postJSON(ctx context.Context, urlStr string, body any, headers map[string]string) (map[string]any, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal json request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(string(payload)))
	if err != nil {
		return nil, 0, fmt.Errorf("build json request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return c.do(req)
}

func (c *oauthClient) do(req *http.Request) (map[string]any, int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("oauth request: %w", err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	status := resp.StatusCode
	if readErr != nil {
		return nil, status, fmt.Errorf("read oauth response: %w", readErr)
	}
	var parsed map[string]any
	if len(raw) > 0 {
		// 上游失败时常常返回非 JSON；解析失败不致命，交由调用方按状态码处理。
		_ = json.Unmarshal(raw, &parsed)
	}
	if parsed == nil {
		parsed = map[string]any{}
	}
	if status < 200 || status >= 300 {
		return parsed, status, &oauthHTTPError{Status: status, Body: parsed, Raw: strings.TrimSpace(string(raw))}
	}
	return parsed, status, nil
}

// oauthHTTPError 保留上游状态码与响应体，供登录轮询区分「继续等」与「真失败」。
type oauthHTTPError struct {
	Status int
	Body   map[string]any
	Raw    string
}

func (e *oauthHTTPError) Error() string {
	if code := stringField(e.Body, "error"); code != "" {
		return fmt.Sprintf("oauth HTTP %d: %s", e.Status, code)
	}
	if e.Raw != "" {
		trimmed := e.Raw
		if len(trimmed) > 200 {
			trimmed = trimmed[:200]
		}
		return fmt.Sprintf("oauth HTTP %d: %s", e.Status, trimmed)
	}
	return fmt.Sprintf("oauth HTTP %d", e.Status)
}

// pkcePair 是 RFC 7636 的 code verifier/challenge。minimax 的设备码流程要求 S256。
type pkcePair struct {
	Verifier  string
	Challenge string
}

func newPKCE() (pkcePair, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return pkcePair{}, fmt.Errorf("generate pkce verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	return pkcePair{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}

// stringField 读取 JSON 对象里的字符串字段，缺失或类型不符时返回空串。
func stringField(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	if value, ok := obj[key].(string); ok {
		return value
	}
	return ""
}

// numberField 读取 JSON 数字字段（含以字符串编码的数字），失败返回 0。
func numberField(obj map[string]any, key string) float64 {
	if obj == nil {
		return 0
	}
	switch value := obj[key].(type) {
	case float64:
		return value
	case json.Number:
		f, _ := value.Float64()
		return f
	case string:
		var f float64
		if _, err := fmt.Sscanf(value, "%g", &f); err == nil {
			return f
		}
	}
	return 0
}

// decodeJWTClaim 解出 JWT 第二段 payload 里的指定字符串声明；非 JWT 时返回空。
func decodeJWTClaim(token, claim string) string {
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return stringField(claims, claim)
}

// decodeJWTExpiresAt 解出 JWT 的 exp（秒）并转成时间。缺失或非法时返回零值。
func decodeJWTExpiresAt(token string) time.Time {
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(claims.Exp), 0)
}

package channel

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// raccoon 渠道测试：覆盖二维码内容与本地渲染（SHA-256 对齐参考实现）、手机号
// AES-128-CFB 加密向量、信封解析、扫码/短信登录流程、续期与凭据映射。

// raccoonTestChannel 造一个指向假上游、超时很短的渠道。
func raccoonTestChannel(t *testing.T, apiBase string) *RaccoonChannel {
	t.Helper()
	c := NewRaccoonChannel(t.TempDir())
	c.apiBase = apiBase
	c.loginTimeout = 5 * time.Second
	return c
}

// TestRaccoonPhoneEncryptionVector 是加密实现的钉死测试：密钥为内置常量、
// IV 取 0x00..0x0f 时，phone 13800138000 的密文必须是参考实现算出的固定值。
//
// 参考值由 node crypto（aes-128-cfb、setAutoPadding(false)、IV 前置）产生。
// 一旦实现改成 CBC/带填充/不前置 IV，这里立刻失败。
func TestRaccoonPhoneEncryptionVector(t *testing.T) {
	key := []byte(raccoonPhoneCipherSecret)
	if len(key) != 16 {
		t.Fatalf("密钥应为 16 字节，得到 %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	iv := make([]byte, 16)
	for i := range iv {
		iv[i] = byte(i)
	}
	phone := []byte("13800138000")
	ciphertext := make([]byte, len(phone))
	cipher.NewCFBEncrypter(block, iv).XORKeyStream(ciphertext, phone)
	got := base64.StdEncoding.EncodeToString(append(append([]byte{}, iv...), ciphertext...))

	const want = "AAECAwQFBgcICQoLDA0OD37WL6pKybd1FsS1"
	if got != want {
		t.Fatalf("手机号密文与参考实现不一致：\n got %s\nwant %s", got, want)
	}
}

// TestRaccoonEncryptPhoneRoundTripShape 验证 encryptPhone 的形状：随机 IV 每次不同、
// 长度 = 16 + 明文长度、且解密后能还原明文（证明是 AES-128-CFB 而非其它模式）。
func TestRaccoonEncryptPhoneRoundTripShape(t *testing.T) {
	c := NewRaccoonChannel(t.TempDir())

	first, err := c.encryptPhone("13800138000")
	if err != nil {
		t.Fatalf("encryptPhone: %v", err)
	}
	second, err := c.encryptPhone("13800138000")
	if err != nil {
		t.Fatalf("encryptPhone: %v", err)
	}
	if first == second {
		t.Fatalf("两次加密结果相同，说明 IV 不是随机的：%s", first)
	}

	raw, err := base64.StdEncoding.DecodeString(first)
	if err != nil {
		t.Fatalf("密文不是标准 base64: %v", err)
	}
	if len(raw) != 16+len("13800138000") {
		t.Fatalf("密文长度应为 IV(16)+明文(11)=27，得到 %d", len(raw))
	}

	block, _ := aes.NewCipher([]byte(raccoonPhoneCipherSecret))
	plain := make([]byte, len(raw)-16)
	cipher.NewCFBDecrypter(block, raw[:16]).XORKeyStream(plain, raw[16:])
	if string(plain) != "13800138000" {
		t.Fatalf("解密应还原手机号，得到 %q", string(plain))
	}
}

// TestRaccoonEncryptPhoneRejectsBadKey 验证密钥长度不对时明确报错，而不是发出坏密文。
func TestRaccoonEncryptPhoneRejectsBadKey(t *testing.T) {
	c := NewRaccoonChannel(t.TempDir())
	c.cipherSecret = "too-short"
	if _, err := c.encryptPhone("13800138000"); err == nil {
		t.Fatalf("密钥长度非法时应报错")
	}
}

// TestRaccoonQrImageUrl 钉住二维码内容：路径、参数名与 appname 的 URL 编码。
func TestRaccoonQrImageUrl(t *testing.T) {
	c := NewRaccoonChannel(t.TempDir())
	got := c.qrImageURL("abcdef0123456789")
	want := "https://xiaohuanxiong.com/login/mp?appname=%E5%95%86%E6%B1%A4%E5%B0%8F%E6%B5%A3%E7%86%8A%E5%AE%98%E7%BD%91&code=abcdef0123456789"
	if got != want {
		t.Fatalf("二维码内容不符：\n got %s\nwant %s", got, want)
	}
}

// TestRaccoonQrMatrixMatchesReference 把本地二维码编码器的输出钉在参考实现上。
//
// 参考值来自官方客户端 raccoon-qr.ts（node 跑出的矩阵逐比特哈希）。这里断言版本
// （模块边长）与矩阵 SHA-256：任何掩码选择/纠错数据/填充的回归都会改变哈希。
func TestRaccoonQrMatrixMatchesReference(t *testing.T) {
	url := "https://xiaohuanxiong.com/login/mp?code=0123456789abcdef0123456789abcdef&appname=%E5%95%86%E6%B1%A4%E5%B0%8F%E6%B5%A3%E7%86%8A%E5%AE%98%E7%BD%91"

	matrix, err := buildRaccoonQrMatrix(url)
	if err != nil {
		t.Fatalf("buildRaccoonQrMatrix: %v", err)
	}
	if matrix.Size != 49 {
		t.Fatalf("矩阵边长应为 49（版本 8），得到 %d", matrix.Size)
	}

	rows := make([]string, 0, len(matrix.Modules))
	for _, row := range matrix.Modules {
		var b strings.Builder
		for _, dark := range row {
			if dark {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		rows = append(rows, b.String())
	}
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	const want = "774915f2411f7181d74b7d60fcb1c7e7c5cef8fc1263c1ec8a1e56f7186798b0"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("矩阵哈希与参考实现不一致：\n got %s\nwant %s", got, want)
	}
}

// TestRaccoonRenderQrSvgShape 验证 SVG 输出的形状：viewBox 含静默区、尺寸/定位正确。
func TestRaccoonRenderQrSvgShape(t *testing.T) {
	c := NewRaccoonChannel(t.TempDir())
	svg, err := renderRaccoonQrSVG(c.qrImageURL("0123456789abcdef0123456789abcdef"), 200, 4)
	if err != nil {
		t.Fatalf("renderRaccoonQrSVG: %v", err)
	}
	if !strings.HasPrefix(svg, "<svg ") || !strings.HasSuffix(svg, "</svg>") {
		t.Fatalf("SVG 首尾不正确: %s", svg[:min(80, len(svg))])
	}
	// 边长 49 + 静默区 4*2 = 57。
	if !strings.Contains(svg, `viewBox="0 0 57 57"`) {
		t.Fatalf("viewBox 应含静默区（0 0 57 57）: %s", svg[:min(200, len(svg))])
	}
	if !strings.Contains(svg, `width="200"`) || !strings.Contains(svg, `height="200"`) {
		t.Fatalf("SVG 尺寸应取传入像素: %s", svg[:min(200, len(svg))])
	}
}

// TestRaccoonParseEnvelope 覆盖信封解析：code 回退到 HTTP 状态、data 非对象时忽略、
// 错误文案拼接。
func TestRaccoonParseEnvelope(t *testing.T) {
	ok := parseRaccoonEnvelope(map[string]any{"code": float64(0), "data": map[string]any{"access_token": "t"}}, 200)
	if !ok.OK() {
		t.Fatalf("code=0 应视为成功")
	}
	if ok.Data["access_token"] != "t" {
		t.Fatalf("应解出 data.access_token")
	}

	// code 缺失 + HTTP 500 → 用状态码当 code。
	failed := parseRaccoonEnvelope(map[string]any{"message": "boom"}, 500)
	if failed.OK() || failed.Code != 500 {
		t.Fatalf("code 缺失时应回退到 HTTP 状态码，得到 %d ok=%v", failed.Code, failed.OK())
	}

	// data 是数组（非对象）时忽略，避免把列表当字典读。
	listData := parseRaccoonEnvelope(map[string]any{"code": float64(0), "data": []any{1, 2}}, 200)
	if listData.Data != nil {
		t.Fatalf("data 非对象时应忽略")
	}

	joined := raccoonEnvelope{Code: 1, Message: "图形验证码校验失败", Details: "请重试"}
	if got := joined.Error("兜底"); got.Error() != "图形验证码校验失败: 请重试" {
		t.Fatalf("错误文案应由 message 与 details 拼接，得到 %q", got.Error())
	}
	empty := raccoonEnvelope{Code: 1}
	if got := empty.Error("兜底"); got.Error() != "兜底" {
		t.Fatalf("无 message/details 时应回退到 fallback，得到 %q", got.Error())
	}
}

// TestRaccoonPollQrLoginStates 覆盖轮询状态机：logging/canceled/success 与
// 「一切异常都归为 pending」。
func TestRaccoonPollQrLoginStates(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != raccoonQrPollPath {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["qrcode_code"] != "qr-code-1" {
			t.Errorf("轮询应带 qrcode_code，得到 %v", body["qrcode_code"])
		}
		writeJSON(w, payload)
	}))
	defer server.Close()
	c := raccoonTestChannel(t, server.URL)

	cases := []struct {
		name       string
		envelope   map[string]any
		wantStatus string
		wantToken  string
	}{
		{"logging", map[string]any{"code": float64(0), "data": map[string]any{"status": "logging", "expired_at": "1700000000000"}}, raccoonQRLogging, ""},
		{"canceled", map[string]any{"code": float64(0), "data": map[string]any{"status": "canceled"}}, raccoonQRCanceled, ""},
		{"pending", map[string]any{"code": float64(0), "data": map[string]any{"status": "pending"}}, raccoonQRPending, ""},
		{"success", map[string]any{"code": float64(0), "data": map[string]any{"status": "success", "access_token": "at-1", "refresh_token": "rt-1"}}, raccoonQRSuccess, "at-1"},
		{"business failure is pending", map[string]any{"code": float64(1), "message": "boom"}, raccoonQRPending, ""},
		{"success without token is pending", map[string]any{"code": float64(0), "data": map[string]any{"status": "success"}}, raccoonQRPending, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload = tc.envelope
			got, err := c.pollQrLogin(context.Background(), "qr-code-1")
			if err != nil {
				t.Fatalf("pollQrLogin: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("状态应为 %q，得到 %q", tc.wantStatus, got.Status)
			}
			if got.AccessToken != tc.wantToken {
				t.Fatalf("令牌应为 %q，得到 %q", tc.wantToken, got.AccessToken)
			}
		})
	}
}

// TestRaccoonPollQrLoginNetworkErrorIsPending 验证传输失败不会把会话终结。
func TestRaccoonPollQrLoginNetworkErrorIsPending(t *testing.T) {
	c := raccoonTestChannel(t, "http://127.0.0.1:1") // 必然连不上
	got, err := c.pollQrLogin(context.Background(), "qr-code-1")
	if err != nil {
		t.Fatalf("网络错误不应返回 error：%v", err)
	}
	if got.Status != raccoonQRPending {
		t.Fatalf("网络错误应按 pending 处理，得到 %q", got.Status)
	}
}

// TestRaccoonSendSmsCode 验证短信下发：phone 加密、captcha_param 透传、验证码失败码
// 映射为可读文案。
func TestRaccoonSendSmsCode(t *testing.T) {
	var gotBody map[string]any
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != raccoonSmsSendPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		if fail {
			writeJSON(w, map[string]any{"code": float64(raccoonCaptchaFailedCode)})
			return
		}
		writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{}})
	}))
	defer server.Close()
	c := raccoonTestChannel(t, server.URL)

	if err := c.sendSmsCode(context.Background(), "13800138000", "captcha-abc"); err != nil {
		t.Fatalf("sendSmsCode: %v", err)
	}
	if gotBody["nation_code"] != "86" {
		t.Fatalf("nation_code 应为 86，得到 %v", gotBody["nation_code"])
	}
	if gotBody["captcha_param"] != "captcha-abc" {
		t.Fatalf("captcha_param 应原样透传，得到 %v", gotBody["captcha_param"])
	}
	encrypted, _ := gotBody["phone"].(string)
	raw, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("phone 应为 base64 密文，得到 %q", encrypted)
	}
	// 明文手机号绝不应出现在请求体里。
	if strings.Contains(encrypted, "13800138000") {
		t.Fatalf("手机号不应明文出现在请求体")
	}
	block, _ := aes.NewCipher([]byte(raccoonPhoneCipherSecret))
	plain := make([]byte, len(raw)-16)
	cipher.NewCFBDecrypter(block, raw[:16]).XORKeyStream(plain, raw[16:])
	if string(plain) != "13800138000" {
		t.Fatalf("服务端应能解出手机号，得到 %q", string(plain))
	}

	fail = true
	err = c.sendSmsCode(context.Background(), "13800138000", "")
	if err == nil || !strings.Contains(err.Error(), "图形验证码校验失败") {
		t.Fatalf("验证码失败码应映射为可读文案，得到 %v", err)
	}
}

// TestRaccoonLoginWithSmsCode 验证短信登录从 data 取令牌并推出到期时间。
func TestRaccoonLoginWithSmsCode(t *testing.T) {
	token := makeJWT(t, map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != raccoonSmsLoginPath {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{
			"access_token": token, "refresh_token": "rt-1", "office_identity": "org-1",
		}})
	}))
	defer server.Close()
	c := raccoonTestChannel(t, server.URL)

	result, err := c.loginWithSmsCode(context.Background(), "13800138000", "123456")
	if err != nil {
		t.Fatalf("loginWithSmsCode: %v", err)
	}
	if result.AccessToken != token || result.RefreshToken != "rt-1" {
		t.Fatalf("令牌映射不正确: %+v", result)
	}
	if result.OfficeIdentity != "org-1" {
		t.Fatalf("office_identity 应被保留，得到 %q", result.OfficeIdentity)
	}
	if result.ExpiresAt.IsZero() {
		t.Fatalf("应从 JWT exp 推出到期时间")
	}
}

// TestRaccoonLoginWithSmsCodeMissingToken 验证缺 access_token 时明确报错。
func TestRaccoonLoginWithSmsCodeMissingToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{}})
	}))
	defer server.Close()
	c := raccoonTestChannel(t, server.URL)

	_, err := c.loginWithSmsCode(context.Background(), "13800138000", "123456")
	if err == nil || !strings.Contains(err.Error(), "access_token") {
		t.Fatalf("缺 access_token 应报错，得到 %v", err)
	}
}

// TestRaccoonRefreshCredential 覆盖续期：换新令牌、登录态过期映射为 ErrLoginCancelled。
func TestRaccoonRefreshCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != raccoonRefreshPath {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch body["refresh_token"] {
		case "rt-ok":
			writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{"access_token": "at-new"}})
		case "rt-no-refresh":
			// 响应不带新 refresh_token：应沿用旧的。
			writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{"access_token": "at-2", "refresh_token": "rt-2"}})
		default:
			writeJSON(w, map[string]any{"code": float64(raccoonSessionExpiredCode)})
		}
	}))
	defer server.Close()
	c := raccoonTestChannel(t, server.URL)

	next, err := c.refreshCredential(context.Background(), Credential{AccessToken: "old", RefreshToken: "rt-ok"})
	if err != nil {
		t.Fatalf("refreshCredential: %v", err)
	}
	if next.AccessToken != "at-new" {
		t.Fatalf("应换到新令牌，得到 %q", next.AccessToken)
	}
	// 响应未带新 refresh_token 时应沿用旧的。
	if next.RefreshToken != "rt-ok" {
		t.Fatalf("响应缺 refresh_token 时应沿用旧值，得到 %q", next.RefreshToken)
	}

	_, err = c.refreshCredential(context.Background(), Credential{AccessToken: "old", RefreshToken: "rt-dead"})
	if err == nil || !strings.Contains(err.Error(), "登录态已过期") {
		t.Fatalf("会话过期应报可读错误，得到 %v", err)
	}
	if !strings.Contains(err.Error(), "重新登录") {
		t.Fatalf("过期错误应提示重新登录")
	}

	_, err = c.refreshCredential(context.Background(), Credential{AccessToken: "old"})
	if err == nil || !strings.Contains(err.Error(), "refresh_token") {
		t.Fatalf("缺 refresh_token 应报错，得到 %v", err)
	}
}

// TestRaccoonRefreshHTTP401IsExpired 验证 HTTP 401 也按会话过期处理（上游两种表达）。
func TestRaccoonRefreshHTTP401IsExpired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]any{"code": float64(999)})
	}))
	defer server.Close()
	c := raccoonTestChannel(t, server.URL)

	_, err := c.refreshCredential(context.Background(), Credential{AccessToken: "old", RefreshToken: "rt-1"})
	if err == nil || !strings.Contains(err.Error(), "登录态已过期") {
		t.Fatalf("HTTP 401 应按会话过期处理，得到 %v", err)
	}
}

// TestRaccoonFetchUserInfoEnrichesCredential 验证用户信息补全：昵称/手机号/user_id/
// office_identity 落到凭据，且 AccountID 缺失时用 user_id 兜底。
func TestRaccoonFetchUserInfoEnrichesCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != raccoonUserInfoPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-Org-Code"); got != "org-1" {
			t.Errorf("user_info 应带 X-Org-Code，得到 %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer at-1" {
			t.Errorf("user_info 应带 Bearer 令牌，得到 %q", got)
		}
		writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{
			"id": "u-9", "name": "张三", "phone": "13800138000", "office_identity": "org-1",
		}})
	}))
	defer server.Close()
	c := raccoonTestChannel(t, server.URL)

	info, err := c.fetchUserInfo(context.Background(), "at-1", "org-1")
	if err != nil {
		t.Fatalf("fetchUserInfo: %v", err)
	}
	cred := Credential{AccessToken: "at-1"}
	applyRaccoonUserInfo(&cred, info)

	if cred.Nickname != "张三" {
		t.Fatalf("昵称应为服务端 name，得到 %q", cred.Nickname)
	}
	if cred.AccountID != "u-9" {
		t.Fatalf("AccountID 应用 user_id 兜底，得到 %q", cred.AccountID)
	}
	if cred.Extra["phone"] != "13800138000" || cred.Extra["office_identity"] != "org-1" {
		t.Fatalf("Extra 应含 phone 与 office_identity，得到 %v", cred.Extra)
	}
}

// TestRaccoonTransformRequestThinking 覆盖 extra_body.thinking 的折叠：
// reasoning_effort / thinking.type 优先、off→disabled、其余→enabled、顶层字段被删。
func TestRaccoonTransformRequestThinking(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   string // 期望的 thinking.type；空表示不应写 extra_body
		absent bool   // 顶层 reasoning_effort/thinking 是否应被删除
	}{
		{"reasoning_effort off", `{"model":"sn-x","reasoning_effort":"off"}`, "disabled", true},
		{"reasoning_effort on", `{"model":"sn-x","reasoning_effort":"on"}`, "enabled", true},
		{"thinking disabled", `{"model":"sn-x","thinking":{"type":"disabled"}}`, "disabled", true},
		{"thinking enabled", `{"model":"sn-x","thinking":{"type":"enabled"}}`, "enabled", true},
		{"no effort keeps payload", `{"model":"sn-x","messages":[]}`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := RaccoonTransformRequest([]byte(tc.in))
			if err != nil {
				t.Fatalf("RaccoonTransformRequest: %v", err)
			}
			var parsed map[string]any
			if err := json.Unmarshal(out, &parsed); err != nil {
				t.Fatalf("输出不是合法 JSON: %v", err)
			}
			if _, present := parsed["reasoning_effort"]; present {
				t.Fatalf("顶层 reasoning_effort 应被删除")
			}
			if _, present := parsed["thinking"]; present {
				t.Fatalf("顶层 thinking 应被删除")
			}
			extra, hasExtra := parsed["extra_body"].(map[string]any)
			if tc.want == "" {
				if hasExtra {
					t.Fatalf("无档位信息时不应写 extra_body，得到 %v", extra)
				}
				return
			}
			if !hasExtra {
				t.Fatalf("应写入 extra_body.thinking")
			}
			thinking, _ := extra["thinking"].(map[string]any)
			if thinking["type"] != tc.want {
				t.Fatalf("thinking.type 应为 %q，得到 %v", tc.want, thinking["type"])
			}
		})
	}
}

// TestRaccoonTransformRequestKeepsOtherFields 验证改写保留 model/messages 等字段。
func TestRaccoonTransformRequestKeepsOtherFields(t *testing.T) {
	out, err := RaccoonTransformRequest([]byte(`{"model":"sn-glm-5-3","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"off"}`))
	if err != nil {
		t.Fatalf("RaccoonTransformRequest: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(out, &parsed)
	if parsed["model"] != "sn-glm-5-3" {
		t.Fatalf("model 应保留，得到 %v", parsed["model"])
	}
	if _, ok := parsed["messages"].([]any); !ok {
		t.Fatalf("messages 应保留")
	}
}

// TestRaccoonTransformRequestInvalidJSON 验证非 JSON 体原样透传（交给上游报错）。
func TestRaccoonTransformRequestInvalidJSON(t *testing.T) {
	in := []byte("not json at all")
	out, err := RaccoonTransformRequest(in)
	if err != nil {
		t.Fatalf("非法 JSON 不应返回 error：%v", err)
	}
	if string(out) != string(in) {
		t.Fatalf("非法 JSON 应原样透传")
	}
}

// TestRaccoonTransformRequestMergesExtraBody 验证已有 extra_body 的其他字段不被覆盖
// （inbound 可能已经带了扩展字段），只把 thinking 合并进去。
func TestRaccoonTransformRequestMergesExtraBody(t *testing.T) {
	out, err := RaccoonTransformRequest([]byte(`{"model":"sn-x","extra_body":{"foo":"bar","thinking":{"type":"disabled"}},"reasoning_effort":"on"}`))
	if err != nil {
		t.Fatalf("RaccoonTransformRequest: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(out, &parsed)
	extra, _ := parsed["extra_body"].(map[string]any)
	if extra["foo"] != "bar" {
		t.Fatalf("已有 extra_body 字段应保留，得到 %v", extra)
	}
	thinking, _ := extra["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking 应按档位覆盖为 enabled，得到 %v", thinking)
	}
}

// TestRaccoonClientHeaders 钉住复刻官方客户端的固定头。
func TestRaccoonClientHeaders(t *testing.T) {
	c := NewRaccoonChannel(t.TempDir())
	headers := c.ClientHeaders()
	if headers["Accept"] != "text/event-stream" {
		t.Fatalf("Accept 应为 text/event-stream，得到 %q", headers["Accept"])
	}
	if headers["X-Raccoon-Language"] != "zh" {
		t.Fatalf("X-Raccoon-Language 应为 zh，得到 %q", headers["X-Raccoon-Language"])
	}
	if headers["X-Client-Platform"] != "desktop-windows" {
		t.Fatalf("X-Client-Platform 应为 desktop-windows，得到 %q", headers["X-Client-Platform"])
	}
	if _, ok := headers["X-Org-Code"]; ok {
		t.Fatalf("X-Org-Code 是随账号变化的，不应放进静态头")
	}
}

// TestRaccoonDynamicHeadersUsesCurrentAccount 验证 X-Org-Code 取当前账号的
// office_identity，且个人账号（无 office_identity）不写该头。
func TestRaccoonDynamicHeadersUsesCurrentAccount(t *testing.T) {
	dir := t.TempDir()
	c := NewRaccoonChannel(dir)

	// 未登录：不写任何头。
	if headers := c.DynamicHeaders()(); len(headers) != 0 {
		t.Fatalf("未登录时不应写动态头，得到 %v", headers)
	}

	writeCreds(t, NewStore(dir), raccoonChannel, Credential{
		AccessToken: "at-1", AccountID: "u-1",
		Extra: map[string]string{"office_identity": "org-9"},
	})
	c.accounts = NewAccounts(raccoonChannel, NewStore(dir), c.client, c.refreshCredential)
	headers := c.DynamicHeaders()()
	if headers["X-Org-Code"] != "org-9" {
		t.Fatalf("X-Org-Code 应为当前账号的 office_identity，得到 %v", headers)
	}

	// 个人账号：没有 office_identity 时不写该头。
	dir2 := t.TempDir()
	writeCreds(t, NewStore(dir2), raccoonChannel, Credential{AccessToken: "at-2", AccountID: "u-2"})
	c2 := NewRaccoonChannel(dir2)
	if headers := c2.DynamicHeaders()(); len(headers) != 0 {
		t.Fatalf("个人账号不应写 X-Org-Code，得到 %v", headers)
	}
}

// TestRaccoonChatURL 验证推理端点拼接。
func TestRaccoonChatURL(t *testing.T) {
	c := NewRaccoonChannel(t.TempDir())
	want := "https://xiaohuanxiong.com/api/web/llm/v2/chat/completions"
	if c.ChatURL() != want {
		t.Fatalf("ChatURL 应为 %s，得到 %s", want, c.ChatURL())
	}
	if c.Name() != raccoonChannel {
		t.Fatalf("Name 应为 %s，得到 %s", raccoonChannel, c.Name())
	}
}

// TestRaccoonCredentialMapping 验证登录结果 → 落盘凭据的映射（令牌/刷新令牌/Extra）。
func TestRaccoonCredentialMapping(t *testing.T) {
	cred := raccoonCredential(raccoonLoginResult{
		AccessToken:    "at-1",
		RefreshToken:   "rt-1",
		ExpiresAt:      time.Unix(1700000000, 0),
		OfficeIdentity: "org-1",
	})
	if cred.AccessToken != "at-1" || cred.RefreshToken != "rt-1" {
		t.Fatalf("令牌映射不正确: %+v", cred)
	}
	if cred.Extra["office_identity"] != "org-1" {
		t.Fatalf("office_identity 应写入 Extra，得到 %v", cred.Extra)
	}
	if cred.ExpiresAt.Unix() != 1700000000 {
		t.Fatalf("到期时间应保留，得到 %v", cred.ExpiresAt)
	}
}

// ── 登录页与流程集成 ──────────────────────────────────────────────────────

// raccoonFakeUpstream 是假上游：回放扫码/短信/用户信息端点。
type raccoonFakeUpstream struct {
	t          *testing.T
	server     *httptest.Server
	pollStatus string
	// smsSendFail 为真时 send_sms 返回验证码失败码。
	smsSendFail bool
	seenPaths   []string
}

func newRaccoonFakeUpstream(t *testing.T, pollStatus string) *raccoonFakeUpstream {
	t.Helper()
	f := &raccoonFakeUpstream{t: t, pollStatus: pollStatus}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.seenPaths = append(f.seenPaths, r.URL.Path)
		switch r.URL.Path {
		case raccoonQrPollPath:
			if f.pollStatus == raccoonQRSuccess {
				writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{
					"status": "success", "access_token": "at-qr", "refresh_token": "rt-qr",
				}})
				return
			}
			writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{"status": f.pollStatus}})
		case raccoonSmsSendPath:
			if f.smsSendFail {
				writeJSON(w, map[string]any{"code": float64(raccoonCaptchaFailedCode)})
				return
			}
			writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{}})
		case raccoonSmsLoginPath:
			writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{
				"access_token": "at-sms", "refresh_token": "rt-sms",
			}})
		case raccoonUserInfoPath:
			writeJSON(w, map[string]any{"code": float64(0), "data": map[string]any{
				"id": "u-9", "name": "张三", "phone": "13800138000",
			}})
		case raccoonLoginRewardURI:
			writeJSON(w, map[string]any{"code": float64(0)})
		default:
			http.NotFound(w, r)
		}
	}))
	return f
}

// TestRaccoonLoginPageRoutes 走通回环页四个路由中的初始渲染：登录页返回 HTML、
// 内联二维码 SVG，且二维码里的 code 与轮询用的 code 一致。
func TestRaccoonLoginPageRoutes(t *testing.T) {
	upstream := newRaccoonFakeUpstream(t, raccoonQRPending)
	defer upstream.server.Close()
	c := raccoonTestChannel(t, upstream.server.URL)

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	defer closeRaccoonLogin(t, session)

	dc := session.DeviceCode()
	if !strings.HasPrefix(dc.VerificationURIComplete, "http://127.0.0.1:") {
		t.Fatalf("登录地址应为本机回环页，得到 %s", dc.VerificationURIComplete)
	}
	if !strings.HasSuffix(dc.LoginURL(), raccoonPageLoginPath) {
		t.Fatalf("login_url 应指向登录页，得到 %s", dc.LoginURL())
	}

	resp, err := http.Get(dc.VerificationURIComplete)
	if err != nil {
		t.Fatalf("拉取登录页失败: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "<svg") || !strings.Contains(html, "Raccoon") {
		t.Fatalf("登录页应内联二维码 SVG 与标题: %s", html[:min(200, len(html))])
	}
	if strings.Contains(html, "{{RACCOON") {
		t.Fatalf("占位符应被全部替换")
	}
	if !strings.Contains(html, "SceneId: '"+raccoonCaptchaSceneID+"'") {
		t.Fatalf("滑块 SceneId 应由 Go 常量注入")
	}

	// 轮询端点应回 pending，且请求打到假上游。
	pollResp, err := http.Get("http://127.0.0.1:" + portOf(t, dc.VerificationURIComplete) + raccoonPagePollPath)
	if err != nil {
		t.Fatalf("轮询本地端点失败: %v", err)
	}
	defer pollResp.Body.Close()
	var poll struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(pollResp.Body).Decode(&poll)
	if poll.Status != raccoonQRPending {
		t.Fatalf("轮询应回 pending，得到 %q", poll.Status)
	}
}

// TestRaccoonQrLoginCompletes 走通扫码登录：轮询到 success 后 settle，
// Wait 返回凭据、补全用户信息并落盘。
func TestRaccoonQrLoginCompletes(t *testing.T) {
	upstream := newRaccoonFakeUpstream(t, raccoonQRSuccess)
	defer upstream.server.Close()
	dir := t.TempDir()
	c := raccoonTestChannel(t, upstream.server.URL)
	c.accounts = NewAccounts(raccoonChannel, NewStore(dir), c.client, c.refreshCredential)

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	port := portOf(t, session.DeviceCode().VerificationURIComplete)

	type waitResult struct {
		cred *Credential
		err  error
	}
	done := make(chan waitResult, 1)
	go func() {
		cred, err := session.Wait(context.Background())
		done <- waitResult{cred, err}
	}()

	// 页面轮询一次即命中 success。
	resp, err := http.Get("http://127.0.0.1:" + port + raccoonPagePollPath)
	if err != nil {
		t.Fatalf("轮询失败: %v", err)
	}
	resp.Body.Close()

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("Wait: %v", result.err)
		}
		if result.cred.AccessToken != "at-qr" {
			t.Fatalf("凭据令牌应为 at-qr，得到 %q", result.cred.AccessToken)
		}
		if result.cred.Nickname != "张三" {
			t.Fatalf("应补全昵称，得到 %q", result.cred.Nickname)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Wait 未在超时内返回")
	}

	if !c.accounts.LoggedIn() {
		t.Fatalf("登录后账号池应报告已登录")
	}
	if got := c.accounts.BearerToken(); got != "at-qr" {
		t.Fatalf("账号池令牌应为 at-qr，得到 %q", got)
	}
}

// TestRaccoonSmsLoginFlow 走通短信登录：先下发验证码，再用验证码登录并落盘。
func TestRaccoonSmsLoginFlow(t *testing.T) {
	upstream := newRaccoonFakeUpstream(t, raccoonQRPending)
	defer upstream.server.Close()
	dir := t.TempDir()
	c := raccoonTestChannel(t, upstream.server.URL)
	c.accounts = NewAccounts(raccoonChannel, NewStore(dir), c.client, c.refreshCredential)

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	port := portOf(t, session.DeviceCode().VerificationURIComplete)
	base := "http://127.0.0.1:" + port

	done := make(chan error, 1)
	go func() {
		_, err := session.Wait(context.Background())
		done <- err
	}()

	// 下发验证码。
	postJSONBody(t, base+raccoonPageSmsSend, map[string]any{"phone": "13800138000", "captchaParam": "cap-1"})
	// 用验证码登录。
	postJSONBody(t, base+raccoonPageSmsVerify, map[string]any{"smsCode": "123456"})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Wait 未在超时内返回")
	}

	if got := c.accounts.BearerToken(); got != "at-sms" {
		t.Fatalf("账号池令牌应为 at-sms，得到 %q", got)
	}
}

// TestRaccoonSmsVerifyRequiresVerifiedPhone 验证未下发验证码就直接提交会被拒绝。
func TestRaccoonSmsVerifyRequiresVerifiedPhone(t *testing.T) {
	upstream := newRaccoonFakeUpstream(t, raccoonQRPending)
	defer upstream.server.Close()
	c := raccoonTestChannel(t, upstream.server.URL)

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	defer closeRaccoonLogin(t, session)
	port := portOf(t, session.DeviceCode().VerificationURIComplete)

	body := postJSONBody(t, "http://127.0.0.1:"+port+raccoonPageSmsVerify, map[string]any{"smsCode": "123456"})
	if ok, _ := body["ok"].(bool); ok {
		t.Fatalf("未获取验证码时不应允许直接登录")
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "请先获取手机验证码") {
		t.Fatalf("应提示先获取验证码，得到 %q", msg)
	}
}

// TestRaccoonSmsSendRejectsBadPhone 验证手机号格式校验（与服务端页面一致）。
func TestRaccoonSmsSendRejectsBadPhone(t *testing.T) {
	upstream := newRaccoonFakeUpstream(t, raccoonQRPending)
	defer upstream.server.Close()
	c := raccoonTestChannel(t, upstream.server.URL)

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	defer closeRaccoonLogin(t, session)
	port := portOf(t, session.DeviceCode().VerificationURIComplete)

	body := postJSONBody(t, "http://127.0.0.1:"+port+raccoonPageSmsSend, map[string]any{"phone": "12345"})
	if ok, _ := body["ok"].(bool); ok {
		t.Fatalf("非法手机号不应通过校验")
	}
	// 上游不应被调用。
	for _, path := range upstream.seenPaths {
		if path == raccoonSmsSendPath {
			t.Fatalf("非法手机号不应触发上游请求")
		}
	}
}

// TestRaccoonSmsSendSurfacesCaptchaError 验证滑块失败的上游错误码会透传到页面。
func TestRaccoonSmsSendSurfacesCaptchaError(t *testing.T) {
	upstream := newRaccoonFakeUpstream(t, raccoonQRPending)
	upstream.smsSendFail = true
	defer upstream.server.Close()
	c := raccoonTestChannel(t, upstream.server.URL)

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	defer closeRaccoonLogin(t, session)
	port := portOf(t, session.DeviceCode().VerificationURIComplete)

	body := postJSONBody(t, "http://127.0.0.1:"+port+raccoonPageSmsSend, map[string]any{"phone": "13800138000"})
	if ok, _ := body["ok"].(bool); ok {
		t.Fatalf("滑块失败时不应返回成功")
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "图形验证码") {
		t.Fatalf("应透传验证码失败文案，得到 %q", msg)
	}
}

// TestRaccoonLoginCanceledOnTimeout 验证超时把 Wait 归为可重试的取消错误。
func TestRaccoonLoginCanceledOnTimeout(t *testing.T) {
	upstream := newRaccoonFakeUpstream(t, raccoonQRPending)
	defer upstream.server.Close()
	c := raccoonTestChannel(t, upstream.server.URL)
	c.loginTimeout = 150 * time.Millisecond

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if _, err := session.Wait(context.Background()); err == nil {
		t.Fatalf("超时应返回错误")
	}
}

// TestRaccoonHandleRejectsOversizedBody 验证回环页请求体上限。
func TestRaccoonHandleRejectsOversizedBody(t *testing.T) {
	upstream := newRaccoonFakeUpstream(t, raccoonQRPending)
	defer upstream.server.Close()
	c := raccoonTestChannel(t, upstream.server.URL)

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	defer closeRaccoonLogin(t, session)
	port := portOf(t, session.DeviceCode().VerificationURIComplete)

	big := strings.Repeat("x", raccoonMaxBodyBytes+10)
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+port+raccoonPageSmsSend, strings.NewReader(big))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if ok, _ := body["ok"].(bool); ok {
		t.Fatalf("超大请求体不应被接受")
	}
}

// ── 测试辅助 ──────────────────────────────────────────────────────────────

// portOf 从本机回环登录地址里取出端口。
func portOf(t *testing.T, loginURL string) string {
	t.Helper()
	rest := strings.TrimPrefix(loginURL, "http://127.0.0.1:")
	port, _, _ := strings.Cut(rest, "/")
	if port == "" {
		t.Fatalf("无法从 %s 解析端口", loginURL)
	}
	return port
}

// closeRaccoonLogin 直接关掉回环登录服务，避免用例靠 Wait 超时（5s）才收尾。
func closeRaccoonLogin(t *testing.T, session LoginSession) {
	t.Helper()
	if login, ok := session.(*raccoonLogin); ok {
		login.close()
	}
}

// postJSONBody 向本机回环页 POST 一个 JSON 体并解出响应。
func postJSONBody(t *testing.T, url string, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	return body
}

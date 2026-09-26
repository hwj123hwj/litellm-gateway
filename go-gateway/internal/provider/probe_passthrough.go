package provider

// 非聊天能力（嵌入、语音转写）的探测：不能拿聊天探测请求打上游——
// 这些模型的上游端点不是 chat/completions，聊天请求只会得到 404/400，
// 误报成离线。按模型能力向对应端点发最小请求，复用同一套结论分类。

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// CapabilityEmbedding / CapabilityTranscription 是非聊天协议的能力词，
// providers.yaml 里声明后由透传端点（/v1/embeddings 等）服务。
const (
	CapabilityEmbedding     = "embedding"
	CapabilityTranscription = "audio_transcription"
)

// hasCapabilityFlag 报告能力列表里是否含指定项。
func hasCapabilityFlag(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

// DeriveUpstreamOrigin 从 provider URL 推导上游源站：
// 兼容两种配置风格——源站（https://api.siliconflow.cn）与完整聊天端点
// （https://host/v1/chat/completions）。透传端点拼接与探测共用此推导，
// 结果总是「不带 /v1 的源站」，调用方在其后拼接 /v1/xxx 端点路径。
func DeriveUpstreamOrigin(url string) string {
	u := strings.TrimSuffix(url, "/")
	u = strings.TrimSuffix(u, "/chat/completions")
	u = strings.TrimSuffix(u, "/v1")
	return strings.TrimSuffix(u, "/")
}

// probePassthrough 向非聊天端点发最小请求并归类探测结论。
// build 构造完整的上游请求；分类逻辑与 probeVia 保持一致：
// 2xx 在线，401/402/403/404 离线（改配置才能恢复），其余降级（可自愈）。
func (w *BoundModelProviderWrapper) probePassthrough(ctx context.Context, path string, build func(ctx context.Context, target string) (*http.Request, error)) ProbeResult {
	client := &http.Client{Timeout: probeTimeout}
	target := DeriveUpstreamOrigin(w.Provider.URL()) + path

	req, err := build(ctx, target)
	if err != nil {
		return ProbeResult{Status: ProbeUnknown, Detail: "构造探测请求失败: " + err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+w.Provider.APIKey())

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return ProbeResult{Status: ProbeDegraded, Detail: boundedErrorSummary(err), Latency: latency}
	}
	defer resp.Body.Close()

	status := classifyProbeStatus(resp.StatusCode)
	detail := "上游返回 2xx"
	if status != ProbeOnline {
		detail = "上游返回 " + http.StatusText(resp.StatusCode)
	}
	return ProbeResult{Status: status, StatusCode: resp.StatusCode, Detail: detail, Latency: latency}
}

// probeEmbedding 向 /v1/embeddings 发最小嵌入请求。
func (w *BoundModelProviderWrapper) probeEmbedding(ctx context.Context) ProbeResult {
	return w.probePassthrough(ctx, "/v1/embeddings", func(ctx context.Context, target string) (*http.Request, error) {
		body, err := json.Marshal(map[string]any{"model": w.boundModel, "input": probePrompt})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
}

// probeTranscription 向 /v1/audio/transcriptions 发一段极短静音 WAV 的转写请求。
// 上游若对音频内容有最低要求，返回的 4xx 也会归类为降级而非离线——
// 只要能区分「上游可达」与「配置错误」就达到探测目的。
func (w *BoundModelProviderWrapper) probeTranscription(ctx context.Context) ProbeResult {
	return w.probePassthrough(ctx, "/v1/audio/transcriptions", func(ctx context.Context, target string) (*http.Request, error) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if err := mw.WriteField("model", w.boundModel); err != nil {
			return nil, err
		}
		fw, err := mw.CreateFormFile("file", "probe.wav")
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(probeWav()); err != nil {
			return nil, err
		}
		if err := mw.Close(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(buf.Bytes()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return req, nil
	})
}

// probeWav 生成 120ms 的 16kHz 单声道 16-bit 静音 WAV，作为转写探测的最小载荷。
func probeWav() []byte {
	const (
		sampleRate = 16000
		durationMs = 120
	)
	samples := sampleRate * durationMs / 1000
	dataSize := samples * 2

	buf := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))           // fmt 块长度
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))            // PCM
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))            // 单声道
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate))   // 采样率
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate*2)) // 字节率
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))            // 块对齐
	_ = binary.Write(buf, binary.LittleEndian, uint16(16))           // 位深
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	buf.Write(make([]byte, dataSize)) // 静音样本
	return buf.Bytes()
}

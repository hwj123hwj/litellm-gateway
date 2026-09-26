package provider

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testWrapper(caps []string, url string) *BoundModelProviderWrapper {
	base := NewOpenAIProvider(&Config{Name: "siliconflow", URL: url, APIKey: "sk-probe-key", UseBearer: true})
	return NewBoundModelProviderWrapper(base, "test/model", caps)
}

func TestProbeEmbeddingCapability(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(b, &payload)
		gotModel = payload.Model
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1]}]}`))
	}))
	defer upstream.Close()

	w := testWrapper([]string{CapabilityEmbedding}, upstream.URL)
	result := w.Probe(context.Background())

	if result.Status != ProbeOnline {
		t.Fatalf("status = %s (%s), want online", result.Status, result.Detail)
	}
	if gotPath != "/v1/embeddings" {
		t.Errorf("upstream path = %q, want /v1/embeddings", gotPath)
	}
	if gotAuth != "Bearer sk-probe-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotModel != "test/model" {
		t.Errorf("model = %q", gotModel)
	}
}

func TestProbeTranscriptionCapability(t *testing.T) {
	var gotPath, gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if mr, err := r.MultipartReader(); err == nil {
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				if part.FormName() == "model" {
					b, _ := io.ReadAll(part)
					gotModel = string(b)
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"text":""}`))
	}))
	defer upstream.Close()

	w := testWrapper([]string{CapabilityTranscription}, upstream.URL)
	result := w.Probe(context.Background())

	if result.Status != ProbeOnline {
		t.Fatalf("status = %s (%s), want online", result.Status, result.Detail)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Errorf("upstream path = %q, want /v1/audio/transcriptions", gotPath)
	}
	if gotModel != "test/model" {
		t.Errorf("multipart model = %q", gotModel)
	}
}

func TestProbeCapability_Upstream404IsOffline(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	w := testWrapper([]string{CapabilityEmbedding}, upstream.URL+"/")
	result := w.Probe(context.Background())
	if result.Status != ProbeOffline || result.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %s code=%d, want offline/404", result.Status, result.StatusCode)
	}
}

func TestDeriveUpstreamOrigin(t *testing.T) {
	cases := map[string]string{
		"https://api.siliconflow.cn":                "https://api.siliconflow.cn",
		"https://api.siliconflow.cn/":               "https://api.siliconflow.cn",
		"https://host/v1/chat/completions":          "https://host",
		"https://host/v1/chat/completions/":         "https://host",
		"http://127.0.0.1:8317/v1/chat/completions": "http://127.0.0.1:8317",
	}
	for in, want := range cases {
		if got := DeriveUpstreamOrigin(in); got != want {
			t.Errorf("DeriveUpstreamOrigin(%q) = %q, want %q", in, got, want)
		}
	}
	if !strings.Contains("x", "x") {
		t.Fatal("unreachable")
	}
	_ = log.New(io.Discard, "", 0)
}

package connection

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProfilesPersistWithoutExposingTokens(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix/admin/health" || r.Header.Get("Authorization") != "Bearer test-secret" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()
	filename := filepath.Join(t.TempDir(), "profiles", "connections.json")
	s, err := New(filename)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Save(context.Background(), Input{Name: "Local", URL: upstream.URL + "/prefix/admin/", Token: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), "test-secret") {
		t.Fatal("public state leaked token")
	}
	info, _ := os.Stat(filename)
	if info.Mode().Perm() != 0600 {
		t.Fatal("secret file permissions", info.Mode())
	}
	restored, err := New(filename)
	if err != nil {
		t.Fatal(err)
	}
	if restored.State().ActiveID != st.ActiveID {
		t.Fatal("active profile not restored")
	}
	_, err = restored.Save(context.Background(), Input{ID: st.ActiveID, Name: "Renamed", URL: upstream.URL + "/prefix"})
	if err != nil {
		t.Fatal("blank token did not retain credential", err)
	}
	next, err := restored.Delete(st.ActiveID)
	if err != nil || len(next.Profiles) != 0 || next.ActiveID != "" {
		t.Fatal(next, err)
	}
}

func TestFailedAuthDoesNotSave(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer upstream.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	_, err := s.Save(context.Background(), Input{Name: "Invalid", URL: upstream.URL, Token: "wrong"})
	if err == nil || len(s.State().Profiles) > 0 {
		t.Fatal("unverified connection persisted")
	}
	for _, raw := range []string{"file:///etc/passwd", "https://token@example.com", "https://example.com?token=secret", "https://example.com#fragment", "localhost:4001"} {
		if _, err := NormalizeURL(raw); err == nil {
			t.Errorf("accepted unsafe address %q", raw)
		}
	}
}

func TestProxyAuthSwitchAndBoundaries(t *testing.T) {
	makeUpstream := func(token string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Set-Cookie", "secret=bad")
			w.Write([]byte(`{"host":"` + token + `"}`))
		}))
	}
	a, b := makeUpstream("alpha"), makeUpstream("beta")
	defer a.Close()
	defer b.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	first, _ := s.Save(context.Background(), Input{Name: "A", URL: a.URL, Token: "alpha"})
	s.Save(context.Background(), Input{Name: "B", URL: b.URL, Token: "beta"})
	request := func(origin, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "gateway://localhost"+path, nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Authorization", "Bearer injected")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := request("mygo://localhost", "/admin/dashboard")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "beta") || w.Header().Get("Set-Cookie") != "" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	s.Select(first.ActiveID)
	w = request("http://mygo.localhost", "/admin/dashboard")
	if !strings.Contains(w.Body.String(), "alpha") {
		t.Fatal("wrong active host")
	}
	for _, p := range []string{"/v1/chat/completions", "/admin/../v1/models", "/admin/%2e%2e/v1/models", "/admin/a%5cb"} {
		if request("mygo://localhost", p).Code != 400 {
			t.Errorf("accepted path %s", p)
		}
	}
	if request("https://untrusted.example", "/admin/dashboard").Code != 403 {
		t.Fatal("untrusted origin accepted")
	}
	s.Delete(first.ActiveID)
	if len(s.State().Profiles) != 1 {
		t.Fatal("delete removed unrelated profile")
	}
}

func TestRedirectCannotLeakCredential(t *testing.T) {
	hits := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer destination.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/health" {
			w.WriteHeader(200)
			return
		}
		http.Redirect(w, r, destination.URL, 302)
	}))
	defer upstream.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	s.Save(context.Background(), Input{Name: "A", URL: upstream.URL, Token: "secret"})
	r := httptest.NewRequest("GET", "gateway://localhost/admin/dashboard", nil)
	r.Header.Set("Origin", "mygo://localhost")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 502 || hits != 0 || w.Header().Get("Location") != "" {
		t.Fatal("redirect followed or escaped", w.Code, hits)
	}
}

func TestProxyStreamsAndCancels(t *testing.T) {
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/health" {
			w.WriteHeader(200)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"type\":\"delta\",\"content\":\"first\"}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	s.Save(context.Background(), Input{Name: "Stream", URL: upstream.URL, Token: "secret"})
	proxy := httptest.NewServer(s)
	defer proxy.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", proxy.URL+"/admin/assistant/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Origin", "mygo://localhost")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal("stream not flushed", err)
	}
	buf := make([]byte, 256)
	n, err := resp.Body.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "first") {
		t.Fatal(n, err)
	}
	cancel()
	resp.Body.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream did not cancel")
	}
}

func TestCancelledProbe(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer upstream.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Test(ctx, Input{Name: "Cancelled", URL: upstream.URL, Token: "secret"}); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("probe wrote configuration")
	}
}

// Ensure request bodies are forwarded intact for mutations, without renderer auth.
func TestProxyMutation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/health" {
			w.WriteHeader(200)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if r.Method != "PUT" || string(body) != `{"providers":["a"]}` {
			t.Error("mutation changed", r.Method, string(body))
		}
		w.WriteHeader(204)
	}))
	defer upstream.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	s.Save(context.Background(), Input{Name: "A", URL: upstream.URL, Token: "secret"})
	r := httptest.NewRequest("PUT", "gateway://localhost/admin/routes/coding", strings.NewReader(`{"providers":["a"]}`))
	r.Header.Set("Origin", "mygo://localhost")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
}

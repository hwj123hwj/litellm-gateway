package connection

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func profile(t *testing.T, s *Store, name, addr, token string) State {
	t.Helper()
	state, err := s.Save(context.Background(), Input{Name: name, URL: addr, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func TestClientSnapshotAndBoundaries(t *testing.T) {
	server := func(token string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("wrong credential")
				w.WriteHeader(401)
				return
			}
			if !strings.HasPrefix(r.URL.Path, "/prefix/admin/") {
				t.Error("lost prefix")
			}
			json.NewEncoder(w).Encode(map[string]string{"host": token})
		}))
	}
	a, b := server("alpha"), server("beta")
	defer a.Close()
	defer b.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	first := profile(t, s, "A", a.URL+"/prefix", "alpha")
	old, _ := s.Client()
	profile(t, s, "B", b.URL+"/prefix", "beta")
	current, _ := s.Client()
	for _, test := range []struct {
		client *Client
		want   string
	}{{old, "alpha"}, {current, "beta"}} {
		var out struct{ Host string }
		if err := test.client.JSON(context.Background(), "GET", "/dashboard", nil, &out); err != nil || out.Host != test.want {
			t.Fatal(out, err)
		}
	}
	for _, endpoint := range []string{"https://evil.example", "//evil.example/api", "/../v1/models", "/%2e%2e/health", "/a%5cb"} {
		if err := current.JSON(context.Background(), "GET", endpoint, nil, nil); err == nil {
			t.Errorf("accepted path %s", endpoint)
		}
	}
	s.Select(first.ActiveID)
	s.Delete(first.ActiveID)
	if len(s.State().Profiles) != 1 {
		t.Fatal("deleted unrelated profile")
	}
}
func TestClientRejectsRedirectAndSanitizesErrors(t *testing.T) {
	var hits atomic.Int32
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer dest.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/health":
			w.WriteHeader(200)
		case "/admin/unauthorized":
			w.WriteHeader(403)
			w.Write([]byte("secret-value"))
		default:
			http.Redirect(w, r, dest.URL, 302)
		}
	}))
	defer srv.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	profile(t, s, "A", srv.URL, "secret-value")
	c, _ := s.Client()
	for _, path := range []string{"/dashboard", "/unauthorized"} {
		err := c.JSON(context.Background(), "GET", path, nil, nil)
		if err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatal("unsafe error", err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("redirect leaked credential")
	}
}
func TestClientMutation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/health" {
			return
		}
		body, _ := io.ReadAll(r.Body)
		if r.Method != "PUT" || string(body) != `{"providers":["a"]}` || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("mutation changed", r.Method, string(body))
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	profile(t, s, "A", srv.URL, "secret")
	c, _ := s.Client()
	if err := c.JSON(context.Background(), "PUT", "/routes/coding", map[string]any{"providers": []string{"a"}}, nil); err != nil {
		t.Fatal(err)
	}
}
func TestClientStreamIncrementalAndCancellation(t *testing.T) {
	cancelled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/health" {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"type\":\"text_delta\",\n"))
		w.(http.Flusher).Flush()
		w.Write([]byte("data: \"content\":\"first\"}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer srv.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	profile(t, s, "A", srv.URL, "secret")
	c, _ := s.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	count := 0
	err := c.Stream(ctx, "/assistant/chat", map[string]string{"message": "hi"}, func(b []byte) bool {
		var e struct{ Content string }
		if json.Unmarshal(b, &e) != nil || e.Content != "first" {
			t.Error("bad SSE", string(b))
		}
		count++
		cancel()
		return true
	})
	if err == nil || count != 1 {
		t.Fatal("cancel ignored", count, err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream did not cancel")
	}
}

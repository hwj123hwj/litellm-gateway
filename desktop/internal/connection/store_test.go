package connection

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestVerifiedSavePersistsOnlyOnCommit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"ok"}`)) }))
	defer srv.Close()
	s, _ := New(filepath.Join(t.TempDir(), "connections.json"))
	commit, err := s.VerifySave(context.Background(), Input{Name: "Verified", URL: srv.URL, Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.State().Profiles) != 0 {
		t.Fatal("verification persisted before UI accepted")
	}
	first, err := commit()
	if err != nil {
		t.Fatal(err)
	}
	second, err := commit()
	if err != nil || first.ActiveID != second.ActiveID || len(s.State().Profiles) != 1 {
		t.Fatal("commit was not idempotent", err)
	}
}

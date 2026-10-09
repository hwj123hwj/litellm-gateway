package connection

import (
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// ServeHTTP streams Admin API responses; the secret never enters the renderer.
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "mygo://localhost" && origin != "http://mygo.localhost" {
		http.Error(w, "仅允许桌面客户端访问", 403)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Vary", "Origin")
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/admin/") || path.Clean(r.URL.Path) != r.URL.Path || strings.Contains(r.URL.Path, "\\") {
		http.Error(w, "不支持的管理路径", 400)
		return
	}
	switch r.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		http.Error(w, "不支持的请求方法", 405)
		return
	}
	s.mu.RLock()
	var profile storedProfile
	for _, p := range s.state.Profiles {
		if p.ID == s.state.ActiveID {
			profile = p
			break
		}
	}
	s.mu.RUnlock()
	if profile.ID == "" {
		http.Error(w, "请先选择网关连接", 401)
		return
	}
	target, _ := url.Parse(profile.URL)
	target.Path = strings.TrimRight(target.Path, "/") + r.URL.Path
	target.RawQuery = r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		http.Error(w, "管理请求无效", 400)
		return
	}
	req.Header.Set("Authorization", "Bearer "+profile.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, "无法连接网关，请检查网络或连接配置", 502)
		return
	}
	defer resp.Body.Close()
	// Redirect locations, cookies and upstream CORS headers must not escape.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		http.Error(w, "网关返回重定向，请使用最终网关地址", 502)
		return
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	buf := make([]byte, 32<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err = w.Write(buf[:n]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return
			}
			break
		}
	}
}

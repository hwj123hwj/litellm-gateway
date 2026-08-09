package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The dashboard is built from web/ and copied into this directory by
// scripts/build-dashboard.sh before release builds. Keeping the generated
// files next to the embed directive also makes a plain `go build` usable from
// a source checkout.
//
//go:embed static/*
var embeddedFiles embed.FS

var staticFiles = mustSubFS(embeddedFiles, "static")

// NewHandler returns the embedded Dashboard handler. Existing files are
// served directly; extensionless paths fall back to index.html so the
// BrowserRouter can handle client-side routes after a page refresh.
func NewHandler() http.Handler {
	fileServer := http.FileServer(http.FS(staticFiles))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		requestPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requestPath == "" || !strings.Contains(path.Base(requestPath), ".") {
			serveIndex(fileServer, w, r)
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}

// NewFaviconHandler returns a no-content handler for browsers that request a
// favicon automatically. The Dashboard does not need a binary icon, but the
// route must be explicit so the request is not mistaken for an unauthorized
// API call and included in business metrics.
func NewFaviconHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func serveIndex(fileServer http.Handler, w http.ResponseWriter, r *http.Request) {
	clone := r.Clone(r.Context())
	// Let FileServer resolve the directory index. Asking it for /index.html
	// would trigger its canonical redirect back to /.
	clone.URL.Path = "/"
	fileServer.ServeHTTP(w, clone)
}

func mustSubFS(files fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(files, dir)
	if err != nil {
		panic("dashboard: embedded static files are invalid: " + err.Error())
	}
	return sub
}

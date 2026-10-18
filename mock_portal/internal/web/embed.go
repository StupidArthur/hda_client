package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

const placeholder = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>Mock Portal</title>
<meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="font:14px system-ui;padding:40px;color:#333">
<h1>Mock Portal</h1>
<p>前端尚未构建。请在 <code>mock_portal/frontend</code> 执行 <code>npm install &amp;&amp; npm run build</code>，再重新编译门户。</p>
</body></html>`

func FS() (fs.FS, error) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, err
	}
	return sub, nil
}

// Handler serves the built SPA and falls back to index.html for client routes.
func Handler() http.Handler {
	sub, err := FS()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(placeholder))
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		name := strings.TrimPrefix(clean, "/")
		if name == "" {
			name = "index.html"
		}
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		if info, err := fs.Stat(sub, name); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.Contains(name, ".") && !strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		clone := r.Clone(r.Context())
		clone.URL.Path = "/"
		clone.URL.RawPath = ""
		fileServer.ServeHTTP(w, clone)
	})
}

// SetNoCache is exposed for tests that inspect the SPA response.
func SetNoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
}

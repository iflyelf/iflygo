package main

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/iflyelf/iflygo/web"
)

// spaHandler 提供内嵌前端静态资源；未命中文件回退 index.html（history 路由）。
func spaHandler() http.Handler {
	dist, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		panic(err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}

		f, err := dist.Open(name)
		if err != nil {
			name = "index.html"
			f, err = dist.Open(name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
		}
		defer f.Close()

		if info, err := f.Stat(); err == nil && info.IsDir() {
			f.Close()
			name = "index.html"
			f, err = dist.Open(name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
		}

		data, err := io.ReadAll(f)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", mimeType(name))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}

func mimeType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".json":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

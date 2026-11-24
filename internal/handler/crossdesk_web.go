package handler

import (
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/iflyelf/iflygo/internal/svc"
	"github.com/iflyelf/iflygo/web"
)

// CrossDeskWeb 提供内嵌 CrossDesk Web 客户端，并注入原生信令配置。
//
// 仅在 CrossDesk 已启用且使用原生 Go 信令时可用；外部 CrossDesk Server 模式
// 继续使用设置中的 web_url。
func CrossDeskWeb(sc *svc.ServiceContext) http.Handler {
	sub, err := fs.Sub(web.CrossDeskFS, "crossdesk")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := sc.ConfigSnapshot().CrossDesk
		if !cfg.Enabled || !cfg.NativeSignaling {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/crossdesk/web")
		if p == "" || p == "/" || p == "/index.html" {
			serveCrossDeskIndex(w, r, sc, index)
			return
		}
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = p
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r2)
	})
}

func serveCrossDeskIndex(w http.ResponseWriter, r *http.Request, sc *svc.ServiceContext, raw []byte) {
	cfg := sc.ConfigSnapshot().CrossDesk
	signaling := crossDeskWebSignalURL(sc, r, cfg.SignalPath)
	ice := []map[string]any{}
	host := strings.TrimSpace(cfg.TurnHost)
	if host == "" {
		if u, err := url.Parse(strings.TrimSpace(cfg.ServerURL)); err == nil {
			host = u.Hostname()
		}
	}
	if host == "" {
		h := r.Host
		if v, _, err := net.SplitHostPort(h); err == nil {
			h = v
		}
		host = strings.Trim(h, "[]")
	}
	port := cfg.TurnPort
	if port <= 0 {
		port = 3478
	}
	if host != "" {
		ice = append(ice, map[string]any{"urls": []string{"stun:" + host + ":" + strconv.Itoa(port)}})
	}
	conf, _ := json.Marshal(map[string]any{
		"signalingUrl": signaling,
		"iceServers":   ice,
		"clientTag":    "web",
	})
	script := "<script>window.CROSSDESK_CONFIG=" + string(conf) + ";</script>\n"
	html := strings.Replace(string(raw), `<script src="control.js`, script+`<script src="control.js`, 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	_, _ = w.Write([]byte(html))
}

func crossDeskWebSignalURL(sc *svc.ServiceContext, r *http.Request, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/crossdesk/signal"
	}
	base := strings.TrimRight(strings.TrimSpace(sc.ConfigSnapshot().Server.ExternalURL), "/")
	if base == "" {
		scheme := "ws"
		if r.TLS != nil {
			scheme = "wss"
		}
		return scheme + "://" + r.Host + path
	}
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + path
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + path
	default:
		return base + path
	}
}

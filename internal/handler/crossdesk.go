package handler

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

// CrossDeskStatus 返回 CrossDesk 集成状态与 Server /stats 探活结果。
func CrossDeskStatus(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := sc.ConfigSnapshot().CrossDesk
		resp := map[string]any{
			"enabled": cfg.Enabled, "server_url": cfg.ServerURL,
			"web_url": cfg.WebURL, "download_url": cfg.DownloadURL,
			"reachable": false,
		}
		if !cfg.Enabled || strings.TrimSpace(cfg.ServerURL) == "" {
			httpx.OK(w, resp)
			return
		}
		base := strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
		statsURL := base + "/stats"
		client := &http.Client{Timeout: 5 * time.Second}
		if cfg.InsecureTLS {
			client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // 仅页面显式开启
		}
		start := time.Now()
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, statsURL, nil)
		if err != nil {
			resp["error"] = err.Error()
			httpx.OK(w, resp)
			return
		}
		req.Header.Set("Accept", "application/json")
		hresp, err := client.Do(req)
		latency := time.Since(start).Milliseconds()
		resp["latency_ms"] = latency
		if err != nil {
			resp["error"] = err.Error()
			httpx.OK(w, resp)
			return
		}
		defer hresp.Body.Close()
		resp["status_code"] = hresp.StatusCode
		body, _ := io.ReadAll(io.LimitReader(hresp.Body, 2<<20))
		if hresp.StatusCode < 200 || hresp.StatusCode >= 300 {
			resp["error"] = fmt.Sprintf("CrossDesk Server 返回 HTTP %d", hresp.StatusCode)
			httpx.OK(w, resp)
			return
		}
		resp["reachable"] = true
		if json.Valid(body) {
			var stats any
			if json.Unmarshal(body, &stats) == nil {
				resp["stats"] = stats
			}
		}
		httpx.OK(w, resp)
	}
}

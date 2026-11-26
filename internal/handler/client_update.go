package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

type clientManifestAsset struct {
	File          string `json:"file"`
	URL           string `json:"url"`
	Package       string `json:"package"`
	PackageSHA256 string `json:"package_sha256"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
}

type clientManifest struct {
	Version string                         `json:"version"`
	Assets  map[string]clientManifestAsset `json:"assets"`
}

var clientManifestCache struct {
	mu       sync.Mutex
	source   string
	manifest *clientManifest
	at       time.Time
}

const clientManifestTTL = 5 * time.Minute

// ClientUpdate 返回客户端升级信息（公开，无敏感数据）。
func ClientUpdate(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := sc.ConfigSnapshot().ClientUpdate
		if !cfg.Enabled || strings.TrimSpace(cfg.ManifestURL) == "" {
			httpx.OK(w, map[string]any{"enabled": false, "update": false})
			return
		}
		osName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("os")))
		arch := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("arch")))
		current := strings.TrimSpace(r.URL.Query().Get("version"))
		if osName == "" || arch == "" {
			httpx.Fail(w, http.StatusBadRequest, "os/arch 必填")
			return
		}
		manifest, err := fetchClientManifest(cfg.ManifestURL)
		if err != nil {
			httpx.OK(w, map[string]any{"enabled": true, "update": false})
			return
		}
		asset, ok := manifest.Assets[osName+"-"+arch]
		if !ok {
			httpx.OK(w, map[string]any{"enabled": true, "update": false})
			return
		}
		force := r.URL.Query().Get("force") == "1"
		if !force && current != "" && current != "dev" && !clientVersionLess(current, manifest.Version) {
			httpx.OK(w, map[string]any{"enabled": true, "update": false, "version": manifest.Version})
			return
		}
		downloadURL := clientAssetURL(cfg.BaseURL, asset)
		if downloadURL == "" {
			httpx.OK(w, map[string]any{"enabled": true, "update": false})
			return
		}
		httpx.OK(w, map[string]any{
			"enabled": true, "update": true, "version": manifest.Version,
			"url": downloadURL, "sha256": asset.SHA256, "size": asset.Size,
		})
	}
}

// ClientDownload 302 跳转到客户端制品（公开）。
func ClientDownload(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := sc.ConfigSnapshot().ClientUpdate
		if !cfg.Enabled {
			http.NotFound(w, r)
			return
		}
		osName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("os")))
		arch := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("arch")))
		if osName == "" || arch == "" {
			httpx.Fail(w, http.StatusBadRequest, "os/arch 必填")
			return
		}
		manifest, err := fetchClientManifest(cfg.ManifestURL)
		if err != nil {
			http.Error(w, "升级清单不可用", http.StatusServiceUnavailable)
			return
		}
		asset, ok := manifest.Assets[osName+"-"+arch]
		if !ok {
			http.NotFound(w, r)
			return
		}
		raw := r.URL.Query().Get("format") == "raw"
		var target string
		if raw {
			target = clientAssetURL(cfg.BaseURL, asset)
		} else {
			target = clientPackageURL(cfg.BaseURL, asset)
		}
		if target == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
	}
}

func fetchClientManifest(source string) (*clientManifest, error) {
	clientManifestCache.mu.Lock()
	if clientManifestCache.source == source && clientManifestCache.manifest != nil &&
		time.Since(clientManifestCache.at) < clientManifestTTL {
		m := clientManifestCache.manifest
		clientManifestCache.mu.Unlock()
		return m, nil
	}
	clientManifestCache.mu.Unlock()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(source)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var manifest clientManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if strings.TrimSpace(manifest.Version) == "" || len(manifest.Assets) == 0 {
		return nil, fmt.Errorf("manifest 缺少 version/assets")
	}
	clientManifestCache.mu.Lock()
	clientManifestCache.source = source
	clientManifestCache.manifest = &manifest
	clientManifestCache.at = time.Now()
	clientManifestCache.mu.Unlock()
	return &manifest, nil
}

func clientAssetURL(base string, asset clientManifestAsset) string {
	if u := validDownloadURL(asset.URL); u != "" {
		return u
	}
	if asset.File == "" {
		return ""
	}
	return joinDownloadURL(base, asset.File)
}

func clientPackageURL(base string, asset clientManifestAsset) string {
	if u := validDownloadURL(asset.URL); u != "" && asset.Package == "" {
		return u
	}
	if asset.Package == "" {
		return clientAssetURL(base, asset)
	}
	return joinDownloadURL(base, asset.Package)
}

func joinDownloadURL(base, file string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || file == "" {
		return ""
	}
	u := base + "/" + strings.TrimLeft(file, "/")
	return validDownloadURL(u)
}

func validDownloadURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	return raw
}

// clientVersionLess 判断 current 是否低于 latest（数字段比较，忽略前缀/后缀）。
func clientVersionLess(current, latest string) bool {
	parse := func(v string) []int {
		v = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(v), "v"))
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			v = v[:i]
		}
		parts := strings.Split(v, ".")
		out := make([]int, 0, len(parts))
		for _, p := range parts {
			n, _ := strconv.Atoi(strings.TrimSpace(p))
			out = append(out, n)
		}
		return out
	}
	a, b := parse(current), parse(latest)
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			return av < bv
		}
	}
	return false
}

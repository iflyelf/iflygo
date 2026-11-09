// Package edge 实现边缘接入：按 Host 把 HTTP 流量反向代理到 overlay 目标。
package edge

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"sync"
	"time"
)

// Route 一条接入映射：HTTP 用 Host，TCP 用 Port。
type Route struct {
	Host   string `json:"host,omitempty"` // 域名（精确或 *.example.com 通配）
	Port   int    `json:"port,omitempty"` // TCP 公网监听端口
	Target string `json:"target"`         // overlayIP:port
}

// Table HTTP 路由表。
type Table struct {
	mu       sync.RWMutex
	exact    map[string]string
	wildcard map[string]string // *.example.com -> target
}

// NewTable 构建 HTTP 路由表（忽略 TCP 路由）。
func NewTable(routes []Route) *Table {
	t := &Table{exact: map[string]string{}, wildcard: map[string]string{}}
	for _, r := range routes {
		if r.Host != "" {
			t.add(r.Host, r.Target)
		}
	}
	return t
}

// FromJSON 从 JSON 加载路由表。
func (t *Table) FromJSON(b []byte) error {
	var routes []Route
	if err := json.Unmarshal(b, &routes); err != nil {
		return err
	}
	t.mu.Lock()
	t.exact = map[string]string{}
	t.wildcard = map[string]string{}
	for _, r := range routes {
		if r.Host != "" {
			t.addLocked(r.Host, r.Target)
		}
	}
	t.mu.Unlock()
	return nil
}

func (t *Table) add(host, target string) {
	t.mu.Lock()
	t.addLocked(host, target)
	t.mu.Unlock()
}

// Set 替换整张路由表。
func (t *Table) Set(routes []Route) {
	t.mu.Lock()
	t.exact = map[string]string{}
	t.wildcard = map[string]string{}
	for _, r := range routes {
		if r.Host != "" {
			t.addLocked(r.Host, r.Target)
		}
	}
	t.mu.Unlock()
}

func (t *Table) addLocked(host, target string) {
	host = strings.ToLower(host)
	if strings.HasPrefix(host, "*.") {
		t.wildcard[host[2:]] = target
	} else {
		t.exact[host] = target
	}
}

// Lookup 查找域名对应的目标。
func (t *Table) Lookup(host string) (string, bool) {
	host = strings.ToLower(host)
	t.mu.RLock()
	defer t.mu.RUnlock()
	if target, ok := t.exact[host]; ok {
		return target, true
	}
	// 通配匹配 *.example.com：逐级去掉最左侧标签，匹配后缀
	rest := host
	for {
		i := strings.IndexByte(rest, '.')
		if i < 0 {
			break
		}
		suffix := rest[i+1:]
		if target, ok := t.wildcard[suffix]; ok {
			return target, true
		}
		rest = suffix
	}
	return "", false
}

// Proxy 反向代理（Host 路由）。
type Proxy struct {
	table *Table
	rp    *httputil.ReverseProxy
}

// NewProxy 创建反向代理。
func NewProxy(table *Table) *Proxy {
	p := &Proxy{table: table}
	p.rp = &httputil.ReverseProxy{
		Director: p.director,
		// 显式连接池与超时：避免后端半开连接无限占用，提升高并发复用率。
		Transport: &http.Transport{
			// overlay 目标是内网地址，禁止走环境变量里的 HTTP 代理。
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          256,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
		},
		// 流式响应（SSE/大文件）及时刷出，避免攒包。
		FlushInterval: 100 * time.Millisecond,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "网关路由未命中或后端不可达: "+err.Error(), http.StatusBadGateway)
		},
	}
	return p
}

func (p *Proxy) director(req *http.Request) {
	originalHost := req.Host
	host := hostOnly(originalHost)
	target, ok := p.table.Lookup(host)
	req.URL.Scheme = "http"
	if !ok {
		req.URL.Host = "127.0.0.1:9" // 不可达占位，触发 ErrorHandler
		return
	}
	req.URL.Host = target
	req.Host = originalHost // 保留原始 Host，供后端 vhost 使用
}

// ServeHTTP 实现 http.Handler。
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.rp.ServeHTTP(w, r)
}

// hostOnly 去掉端口。
func hostOnly(hostPort string) string {
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		return h
	}
	return hostPort
}

// ErrRouteNotFound 路由未命中。
var ErrRouteNotFound = errors.New("route not found")

// LoadRoutesFile 从 JSON 文件读取路由表。
func LoadRoutesFile(path string) ([]Route, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var routes []Route
	if err := json.Unmarshal(b, &routes); err != nil {
		return nil, err
	}
	return routes, nil
}

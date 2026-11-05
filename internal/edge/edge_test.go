package edge_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iflyelf/iflygo/internal/edge"
)

func TestTableLookup(t *testing.T) {
	tbl := edge.NewTable([]edge.Route{
		{Host: "nas.example.com", Target: "10.0.0.2:5000"},
		{Host: "*.wild.example.com", Target: "10.0.0.3:8080"},
	})
	if v, ok := tbl.Lookup("nas.example.com"); !ok || v != "10.0.0.2:5000" {
		t.Fatalf("精确匹配失败: %q %v", v, ok)
	}
	if v, ok := tbl.Lookup("foo.wild.example.com"); !ok || v != "10.0.0.3:8080" {
		t.Fatalf("通配匹配失败: %q %v", v, ok)
	}
	if _, ok := tbl.Lookup("wild.example.com"); ok {
		t.Fatal("通配不应匹配裸域名")
	}
	if _, ok := tbl.Lookup("unknown.example.com"); ok {
		t.Fatal("未知域名不应命中")
	}
}

func TestProxyRouting(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Host))
	}))
	defer backend.Close()
	target := strings.TrimPrefix(backend.URL, "http://")

	proxy := edge.NewProxy(edge.NewTable([]edge.Route{{Host: "nas.example.com", Target: target}}))
	front := httptest.NewServer(proxy)
	defer front.Close()

	req, _ := http.NewRequest(http.MethodGet, front.URL+"/", nil)
	req.Host = "nas.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "nas.example.com" {
		t.Fatalf("后端应看到原始 Host，got %q", body)
	}
}

func TestProxyRouteNotFound(t *testing.T) {
	proxy := edge.NewProxy(edge.NewTable(nil))
	front := httptest.NewServer(proxy)
	defer front.Close()

	req, _ := http.NewRequest(http.MethodGet, front.URL+"/", nil)
	req.Host = "unknown.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", resp.StatusCode)
	}
}

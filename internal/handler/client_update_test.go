package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/svc"
)

func TestClientVersionLess(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "1.2.3", false},
		{"1.10.0", "1.9.9", false},
		{"v2.0.0", "2.0.1", true},
		{"dev", "1.0.0", true},
		{"1.0.0-rc1", "1.0.0", false},
	}
	for _, c := range cases {
		if got := clientVersionLess(c.current, c.latest); got != c.want {
			t.Fatalf("clientVersionLess(%q,%q)=%v want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestClientUpdateHandler(t *testing.T) {
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": "2.1.0",
			"assets": map[string]any{
				"linux-amd64": map[string]any{
					"file": "iflygo-client-linux-amd64", "sha256": "abc123", "size": 123,
					"package": "iflygo-client-linux-amd64.tar.gz", "package_sha256": "def456",
				},
			},
		})
	}))
	defer manifest.Close()
	cfg := &config.Config{ClientUpdate: config.ClientUpdateConfig{
		Enabled: true, ManifestURL: manifest.URL, BaseURL: "https://downloads.example.com",
	}}
	sc := &svc.ServiceContext{Config: cfg}

	req := httptest.NewRequest(http.MethodGet, "/api/client/update?os=linux&arch=amd64&version=2.0.0", nil)
	rec := httptest.NewRecorder()
	ClientUpdate(sc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			Enabled bool   `json:"enabled"`
			Update  bool   `json:"update"`
			Version string `json:"version"`
			URL     string `json:"url"`
			SHA256  string `json:"sha256"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Data.Enabled || !env.Data.Update || env.Data.Version != "2.1.0" ||
		env.Data.URL != "https://downloads.example.com/iflygo-client-linux-amd64" ||
		env.Data.SHA256 != "abc123" {
		t.Fatalf("unexpected update response: %+v", env.Data)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/client/update?os=linux&arch=amd64&version=2.1.0", nil)
	rec = httptest.NewRecorder()
	ClientUpdate(sc).ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Update {
		t.Fatal("same version should not update")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/client/download?os=linux&arch=amd64", nil)
	rec = httptest.NewRecorder()
	ClientDownload(sc).ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://downloads.example.com/iflygo-client-linux-amd64.tar.gz" {
		t.Fatalf("unexpected download redirect: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

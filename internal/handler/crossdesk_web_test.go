package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/svc"
)

func TestCrossDeskWebInjectedConfig(t *testing.T) {
	cfg := config.Default()
	cfg.CrossDesk.Enabled = true
	cfg.CrossDesk.NativeSignaling = true
	cfg.Server.ExternalURL = "https://iflygo.example.com"
	sc := &svc.ServiceContext{Config: cfg}
	h := CrossDeskWeb(sc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "https://iflygo.example.com/crossdesk/web/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("index 状态码=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"window.CROSSDESK_CONFIG=", "wss://iflygo.example.com/crossdesk/signal", "stun:"} {
		if !strings.Contains(body, want) {
			t.Fatalf("index 缺少 %q", want)
		}
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "https://iflygo.example.com/crossdesk/web/web_client.js", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "CrossDesk") {
		t.Fatalf("静态资源服务失败: code=%d", rec.Code)
	}

	cfg.CrossDesk.NativeSignaling = false
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "https://iflygo.example.com/crossdesk/web/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("原生信令关闭时应 404，实际 %d", rec.Code)
	}
}

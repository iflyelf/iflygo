package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iflyelf/iflygo/internal/auth"
	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/store"
	"github.com/iflyelf/iflygo/internal/svc"
)

func installMonitorTestContext(t *testing.T) *svc.ServiceContext {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	stmts := []string{
		`CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '', updated_at TEXT)`,
		`CREATE TABLE enroll_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT, token_hash TEXT NOT NULL DEFAULT '',
			token_prefix TEXT NOT NULL DEFAULT '', token_type TEXT NOT NULL DEFAULT 'one_time',
			expires_at TEXT, max_uses INTEGER NOT NULL DEFAULT 0, used_count INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1, allowed_cidrs TEXT NOT NULL DEFAULT '[]',
			created_by TEXT NOT NULL DEFAULT '', tenant_id INTEGER NOT NULL DEFAULT 1,
			name TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Server: config.ServerConfig{ExternalURL: "https://iflygo.example.com"}}
	return &svc.ServiceContext{Config: cfg, Store: &store.Store{DB: db, Dialect: store.SQLite}}
}

func TestInstallMonitorToken(t *testing.T) {
	sc := installMonitorTestContext(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agent/install-monitor/token?format=json", nil)
	rec := httptest.NewRecorder()
	InstallMonitorTokenGet(sc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			Supported   bool   `json:"supported"`
			BaseURL     string `json:"base_url"`
			LinuxURL    string `json:"linux_url"`
			WindowsURL  string `json:"windows_url"`
			TokenPrefix string `json:"token_prefix"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Data.Supported || env.Data.BaseURL != "https://iflygo.example.com" ||
		!strings.Contains(env.Data.LinuxURL, "install-monitor.sh") ||
		!strings.Contains(env.Data.WindowsURL, "install-windows-monitor.exe") ||
		!strings.HasPrefix(env.Data.TokenPrefix, "enr_auto_") {
		t.Fatalf("unexpected info: %+v", env.Data)
	}

	plainReq := httptest.NewRequest(http.MethodGet, "/api/agent/install-monitor/token", nil)
	plainRec := httptest.NewRecorder()
	InstallMonitorTokenGet(sc).ServeHTTP(plainRec, plainReq)
	token := strings.TrimSpace(plainRec.Body.String())
	if !strings.HasPrefix(token, "enr_auto_") {
		t.Fatalf("bad plain token: %q", token)
	}

	rotateReq := httptest.NewRequest(http.MethodPost, "/api/agent/install-monitor/token", nil)
	rotateReq = rotateReq.WithContext(auth.WithUser(rotateReq.Context(), &auth.User{
		ID: "1", Username: "admin", IsAdmin: true, TenantID: 1, Roles: []string{auth.RoleAdmin},
	}))
	rotateRec := httptest.NewRecorder()
	InstallMonitorTokenCreate(sc).ServeHTTP(rotateRec, rotateReq)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rotateRec.Code, rotateRec.Body.String())
	}
	plainRec = httptest.NewRecorder()
	InstallMonitorTokenGet(sc).ServeHTTP(plainRec, plainReq)
	if again := strings.TrimSpace(plainRec.Body.String()); again == token {
		t.Fatal("rotate did not change token")
	}
}

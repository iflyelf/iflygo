package handler

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/iflyelf/iflygo/internal/auth"
	"github.com/iflyelf/iflygo/internal/crossdesk"
	"github.com/iflyelf/iflygo/internal/store"
	"github.com/iflyelf/iflygo/internal/svc"
)

func TestCrossDeskJoinTicket(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ticket.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE crossdesk_devices (device_id TEXT PRIMARY KEY, password_hash TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT 'desktop', online INTEGER NOT NULL DEFAULT 0, last_seen TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT '', login_public_key TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crossdesk_join_tickets (token_hash TEXT PRIMARY KEY, device_id TEXT NOT NULL, expires_at INTEGER NOT NULL DEFAULT 0, used_at INTEGER NOT NULL DEFAULT 0, created_by TEXT NOT NULL DEFAULT '', created_ip TEXT NOT NULL DEFAULT '', used_ip TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO crossdesk_devices (device_id,kind) VALUES ('123456789','desktop')`); err != nil {
		t.Fatal(err)
	}
	sc := &svc.ServiceContext{Store: &store.Store{DB: db, Dialect: store.SQLite}}
	req := httptest.NewRequest("POST", "/api/crossdesk/devices/123456789/join-ticket", nil)
	req.SetPathValue("id", "123456789")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.User{Username: "admin", IsAdmin: true}))
	rec := httptest.NewRecorder()
	CrossDeskJoinTicket(sc).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			JoinTicket string `json:"join_ticket"`
			JoinURL    string `json:"join_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.JoinTicket) != 64 || env.Data.JoinURL == "" {
		t.Fatalf("票据响应不正确: %+v", env.Data)
	}
	if !crossdesk.NewSQLStore(sc.Store).ConsumeJoinTicket("123456789", env.Data.JoinTicket, "127.0.0.1") {
		t.Fatal("票据应可消费一次")
	}
	if crossdesk.NewSQLStore(sc.Store).ConsumeJoinTicket("123456789", env.Data.JoinTicket, "127.0.0.1") {
		t.Fatal("票据不应可重复消费")
	}
}

package handler

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/iflyelf/iflygo/internal/store"
	"github.com/iflyelf/iflygo/internal/svc"
)

func TestNATStateRetryPersistence(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "nat-state.db"))
	if err != nil {
		t.Fatalf("打开 SQLite 失败: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE gateway_nat_state (
		network_id INTEGER NOT NULL, mode TEXT NOT NULL, fingerprint TEXT NOT NULL DEFAULT '',
		server_node_id INTEGER NOT NULL DEFAULT 0, gateway TEXT NOT NULL DEFAULT '',
		host_ip TEXT NOT NULL DEFAULT '', task_id INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '',
		retry_count INTEGER NOT NULL DEFAULT 0, alerted_at TEXT NOT NULL DEFAULT '',
		last_attempt_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (network_id, mode)
	)`)
	if err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	sc := &svc.ServiceContext{Store: &store.Store{DB: db, Dialect: store.SQLite}}

	want := natState{
		Fingerprint: "fp-1", Status: "failed", RetryCount: 3,
		LastAttempt: "2026-11-13T00:00:00Z", LastError: "iptables failed",
		AlertedAt: "2026-11-13T00:10:00Z", TaskID: 7,
		ServerNodeID: 9, Gateway: "10.77.0.1", HostIP: "10.0.0.9",
	}
	saveNATState(sc, 42, "full", want)
	got, ok := loadNATState(sc, 42, "full")
	if !ok {
		t.Fatal("NAT 状态未落库")
	}
	if got.Fingerprint != want.Fingerprint || got.Status != want.Status ||
		got.RetryCount != want.RetryCount || got.TaskID != want.TaskID ||
		got.AlertedAt != want.AlertedAt || got.Gateway != want.Gateway ||
		got.HostIP != want.HostIP || got.LastError != want.LastError {
		t.Fatalf("NAT 状态往返不一致:\n got=%+v\nwant=%+v", got, want)
	}
}

package handler

import (
	"path/filepath"
	"testing"

	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/store"
	"github.com/iflyelf/iflygo/internal/svc"
)

func TestNATStateRetryPersistence(t *testing.T) {
	cfg := config.Default()
	cfg.Database.Driver = "sqlite"
	cfg.Database.SQLitePath = filepath.Join(t.TempDir(), "nat-state.db")
	st, err := store.Open(cfg)
	if err != nil {
		t.Fatalf("打开 SQLite 失败: %v", err)
	}
	defer st.Close()
	sc := &svc.ServiceContext{Store: st}

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

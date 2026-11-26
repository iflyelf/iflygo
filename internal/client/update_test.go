package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckUpdateAndApply(t *testing.T) {
	newContent := []byte("new-client-binary")
	sum := sha256.Sum256(newContent)
	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(newContent)
	}))
	defer download.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/client/update" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "message": "ok",
			"data": map[string]any{
				"enabled": true, "update": true, "version": "9.9.9",
				"url": download.URL, "sha256": hex.EncodeToString(sum[:]), "size": len(newContent),
			},
		})
	}))
	defer api.Close()

	info, err := CheckUpdate(context.Background(), api.URL, "1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Update || info.Version != "9.9.9" {
		t.Fatalf("unexpected info: %+v", info)
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "iflygo-client")
	if err := os.WriteFile(bin, []byte("old-client"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ApplyUpdate(context.Background(), info, bin, dir); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newContent) {
		t.Fatalf("binary not replaced: %q", got)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "backup", "*"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("backup missing: %v %v", backups, err)
	}
	old, err := os.ReadFile(backups[0])
	if err != nil || string(old) != "old-client" {
		t.Fatalf("backup content: %q err=%v", old, err)
	}
}

func TestApplyUpdateRejectsBadHash(t *testing.T) {
	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered"))
	}))
	defer download.Close()
	dir := t.TempDir()
	bin := filepath.Join(dir, "iflygo-client")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	info := &UpdateInfo{Update: true, URL: download.URL, SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}
	if err := ApplyUpdate(context.Background(), info, bin, dir); err == nil {
		t.Fatal("bad hash accepted")
	}
	got, _ := os.ReadFile(bin)
	if string(got) != "old" {
		t.Fatalf("binary changed after failed update: %q", got)
	}
}

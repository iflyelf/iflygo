package hostmerge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/migrate"
	"github.com/iflyelf/iflygo/internal/store"
)

// TestAutoMergeSQLite 验证双库 SQL（数组/JSON/NULL 合并、UPDATE ... FROM）在 SQLite 下可用。
func TestAutoMergeSQLite(t *testing.T) {
	cfg := config.Default()
	cfg.Database.Driver = "sqlite"
	cfg.Database.SQLitePath = filepath.Join(t.TempDir(), "hostmerge.db")
	s, err := store.Open(cfg)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := migrate.AutoMigrate(s.DB, migrate.DialectSQLite); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now()
	if _, err := s.DB.Exec(`
		INSERT INTO hosts (ip, hostname, tunnel_ip, tags, source_services, custom_fields,
			tenant_id, source, agent_status, last_seen)
		VALUES ('10.200.0.1','merge-a','10.200.0.9',$1,$2,'{"a":"1"}',1,'manual','unknown',NULL)`,
		store.Array([]string{"a"}), store.Array([]string{"svc1"})); err != nil {
		t.Fatalf("insert A: %v", err)
	}
	if _, err := s.DB.Exec(`
		INSERT INTO hosts (ip, hostname, exit_ip, tunnel_ip, tags, source_services, custom_fields,
			tenant_id, source, agent_id, agent_status, last_seen)
		VALUES ('10.200.0.2','merge-b','10.200.0.2','10.200.0.9',$1,$2,'{"b":"2"}',1,'agent','ag-1','online',$3)`,
		store.Array([]string{"b"}), store.Array([]string{"svc2"}), now); err != nil {
		t.Fatalf("insert B: %v", err)
	}

	n, err := AutoMergeAll(context.Background(), s.DB)
	if err != nil {
		t.Fatalf("AutoMergeAll: %v", err)
	}
	if n != 1 {
		t.Fatalf("应合并 1 台，实际 %d", n)
	}

	var count int
	var tagsRaw, svcRaw, customRaw, agentID, lastSeen string
	if err := s.DB.QueryRow(`
		SELECT COUNT(*), COALESCE(MAX(tags),'[]'), COALESCE(MAX(source_services),'[]'),
		       COALESCE(MAX(custom_fields),'{}'), COALESCE(MAX(agent_id),''), COALESCE(MAX(last_seen),'')
		FROM hosts`).Scan(&count, &tagsRaw, &svcRaw, &customRaw, &agentID, &lastSeen); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("应剩 1 台，实际 %d", count)
	}
	var tags, svcs []string
	var custom map[string]string
	if err := json.Unmarshal([]byte(tagsRaw), &tags); err != nil {
		t.Fatalf("tags: %v (%s)", err, tagsRaw)
	}
	if err := json.Unmarshal([]byte(svcRaw), &svcs); err != nil {
		t.Fatalf("services: %v (%s)", err, svcRaw)
	}
	if err := json.Unmarshal([]byte(customRaw), &custom); err != nil {
		t.Fatalf("custom: %v (%s)", err, customRaw)
	}
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("tags 未合并: %v", tags)
	}
	if len(svcs) != 2 || svcs[0] != "svc1" || svcs[1] != "svc2" {
		t.Fatalf("source_services 未合并: %v", svcs)
	}
	if custom["a"] != "1" || custom["b"] != "2" {
		t.Fatalf("custom_fields 未合并: %v", custom)
	}
	if agentID != "ag-1" {
		t.Fatalf("agent_id 未合并: %q", agentID)
	}
	if lastSeen == "" {
		t.Fatalf("last_seen 被 NULL 覆盖")
	}
}

package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iflyelf/iflygo/internal/config"
)

// TestPostgresIntegration 在 CI（PostgreSQL service）下校验迁移 + 基本读写。
// 本地无 DATABASE_URL 时自动跳过（与 upgrade_mgr 集成测试约定一致）。
func TestPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL 未设置，跳过 PostgreSQL 集成测试")
	}
	cfg := config.Default()
	cfg.Database.Driver = "postgres"
	cfg.Database.DSN = dsn

	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer s.Close()

	// 每次使用唯一名称并清理：CI 会连续跑两遍（普通 + -race），固定名称会触发唯一约束。
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	name := "pgtest-" + suffix
	retName := "pgtest-returning-" + suffix
	t.Cleanup(func() {
		_, _ = s.DB.Exec(s.Rebind(`DELETE FROM networks WHERE name IN ($1,$2)`), name, retName)
	})

	if _, err := s.DB.Exec(s.Rebind(`INSERT INTO networks (name, cidr4, cidr6, gateway4, gateway6, dns, mtu, nebula, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`),
		name, "10.0.0.0/16", "", "10.0.0.1", "", "", 1300, "", ""); err != nil {
		t.Fatalf("insert network: %v", err)
	}
	// RETURNING id 是 handler 的标准写法（PostgreSQL 不支持 LastInsertId）
	var nid int64
	if err := s.DB.QueryRow(s.Rebind(`INSERT INTO networks (name, cidr4) VALUES ($1,$2) RETURNING id`), retName, "10.1.0.0/16").Scan(&nid); err != nil {
		t.Fatalf("insert returning id: %v", err)
	}
	if nid == 0 {
		t.Fatalf("RETURNING id 应为非 0，got %d", nid)
	}
	var n int
	if err := s.DB.QueryRow(s.Rebind(`SELECT COUNT(*) FROM networks WHERE name = $1`), name).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 network, got %d", n)
	}
}

func TestSQLiteCompatFunctions(t *testing.T) {
	registerSQLiteFuncs()
	db, err := sql.Open("sqlite", sqliteDSN(filepath.Join(t.TempDir(), "compat.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`SELECT pg_advisory_xact_lock(hashtext('agent:1.2.3.4'))`,
		`SELECT pg_try_advisory_lock(hashtext('gateway'))`,
		`SELECT now()`,
	} {
		var v any
		if err := db.QueryRow(q).Scan(&v); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	var ok bool
	if err := db.QueryRow(`SELECT inet_in_list('10.1.2.3', '["10.0.0.0/8","192.168.0.0/16"]')`).Scan(&ok); err != nil || !ok {
		t.Fatalf("inet_in_list 命中失败: ok=%v err=%v", ok, err)
	}
	if err := db.QueryRow(`SELECT inet_in_list('8.8.8.8', '["10.0.0.0/8"]')`).Scan(&ok); err != nil || ok {
		t.Fatalf("inet_in_list 不应命中: ok=%v err=%v", ok, err)
	}
	var n int64
	if err := db.QueryRow(`SELECT cardinality('["a","b"]')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("cardinality 失败: n=%d err=%v", n, err)
	}
}

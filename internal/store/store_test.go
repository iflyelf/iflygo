package store

import (
	"os"
	"testing"

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

	if _, err := s.DB.Exec(s.Rebind(`INSERT INTO networks (name, cidr4, cidr6, gateway4, gateway6, dns, mtu, nebula, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`),
		"pgtest", "10.0.0.0/16", "", "10.0.0.1", "", "", 1300, "", ""); err != nil {
		t.Fatalf("insert network: %v", err)
	}
	// RETURNING id 是 handler 的标准写法（PostgreSQL 不支持 LastInsertId）
	var nid int64
	if err := s.DB.QueryRow(s.Rebind(`INSERT INTO networks (name, cidr4) VALUES ($1,$2) RETURNING id`), "pgtest-returning", "10.1.0.0/16").Scan(&nid); err != nil {
		t.Fatalf("insert returning id: %v", err)
	}
	if nid == 0 {
		t.Fatalf("RETURNING id 应为非 0，got %d", nid)
	}
	var n int
	if err := s.DB.QueryRow(s.Rebind(`SELECT COUNT(*) FROM networks WHERE name = $1`), "pgtest").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 network, got %d", n)
	}
}

package liaison

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// TestDBRegistrySQLite 验证双库 DDL：SQLite 下显式 ID 可写入、可读回、可关闭。
func TestDBRegistrySQLite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1) // 内存库每个连接独立，固定单连接

	r := newDBRegistry(db)
	r.ensureTable()

	id := newDBConnID()
	if id <= 0 {
		t.Fatalf("ID 应为正数: %d", id)
	}
	if _, err := db.Exec(`INSERT INTO liaison_db_connections (id,name,driver,dsn) VALUES ($1,$2,$3,$4)`,
		id, "local", "sqlite", ":memory:"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var got int64
	if err := db.QueryRow(`SELECT id FROM liaison_db_connections WHERE id=$1`, id).Scan(&got); err != nil {
		t.Fatalf("select id: %v", err)
	}
	if got != id {
		t.Fatalf("id 不匹配: got %d want %d", got, id)
	}
	conn, err := r.get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if conn == nil {
		t.Fatalf("连接池为空")
	}
	r.close(id)
	r.mu.Lock()
	_, cached := r.cache[id]
	r.mu.Unlock()
	if cached {
		t.Fatalf("close 后不应仍在缓存")
	}
}

// TestPolicyAdapterSQLite 验证策略表在 SQLite 下可增删查。
func TestPolicyAdapterSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1) // 内存库每个连接独立，固定单连接
	a := NewPolicyAdapter(db)
	a.ensureTable()
	if err := a.AddPolicy("p", "p_test", []string{"alice", "files:*", "read"}); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	rows, err := db.Query(`SELECT ptype,v0,v1,v2 FROM liaison_policies`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatalf("策略未写入")
	}
	var ptype, v0, v1, v2 string
	if err := rows.Scan(&ptype, &v0, &v1, &v2); err != nil {
		_ = rows.Close()
		t.Fatalf("scan: %v", err)
	}
	_ = rows.Close() // 单连接内存库：先释放连接，避免后续 DELETE 等锁
	if ptype != "p_test" || v0 != "alice" || v1 != "files:*" || v2 != "read" {
		t.Fatalf("unexpected: %s %s %s %s", ptype, v0, v1, v2)
	}
	if err := a.RemovePolicy("p", "p_test", []string{"alice", "files:*", "read"}); err != nil {
		t.Fatalf("RemovePolicy: %v", err)
	}
}

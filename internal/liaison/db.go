package liaison

import (
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	_ "github.com/lib/pq"  // PostgreSQL 驱动
	_ "modernc.org/sqlite" // SQLite 驱动

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/store"
)

// Web DB：管理外部数据库连接，执行只读/查询 SQL（结果集返回 JSON）。
// 连接配置存库（liaison_db_connections），连接池按需建立并缓存。

type dbConnEntry struct {
	Name   string `json:"name"`
	Driver string `json:"driver"` // postgres / sqlite
	DSN    string `json:"dsn"`
}

// dbRegistry 管理数据库连接池缓存。
type dbRegistry struct {
	db    *sql.DB
	mu    sync.Mutex
	once  sync.Once
	cache map[int64]*sql.DB
}

func newDBRegistry(db *sql.DB) *dbRegistry {
	return &dbRegistry{db: db, cache: map[int64]*sql.DB{}}
}

func (r *dbRegistry) ensureTable() {
	r.once.Do(func() {
		_, _ = r.db.Exec(store.SQL(`
			CREATE TABLE IF NOT EXISTS liaison_db_connections (
				id     BIGSERIAL PRIMARY KEY,
				name   VARCHAR(128) NOT NULL DEFAULT '',
				driver VARCHAR(32)  NOT NULL DEFAULT 'postgres',
				dsn    TEXT NOT NULL DEFAULT ''
			)`))
	})
}

// handleDBList 列出数据库连接（DSN 脱敏）。
func (s *Service) handleDBList(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "db:*", ActRead) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	s.dbReg.ensureTable()
	rows, err := s.db.Query(`SELECT id, name, driver, dsn FROM liaison_db_connections ORDER BY id`)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, driver, dsn string
		if err := rows.Scan(&id, &name, &driver, &dsn); err != nil {
			continue
		}
		out = append(out, map[string]any{"id": id, "name": name, "driver": driver, "dsn": maskDSN(dsn)})
	}
	httpx.OK(w, out)
}

// handleDBCreate 新建数据库连接。
func (s *Service) handleDBCreate(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "db:*", ActWrite) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	var req dbConnEntry
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Name == "" || req.DSN == "" {
		httpx.Fail(w, http.StatusBadRequest, "name/driver/dsn 必填")
		return
	}
	if req.Driver == "" {
		req.Driver = "postgres"
	}
	s.dbReg.ensureTable()
	var id int64
	err := s.db.QueryRow(`INSERT INTO liaison_db_connections (name, driver, dsn) VALUES ($1,$2,$3) RETURNING id`,
		req.Name, req.Driver, req.DSN).Scan(&id)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, map[string]any{"id": id})
}

// handleDBDelete 删除数据库连接。
func (s *Service) handleDBDelete(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "db:*", ActWrite) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	id := pathID(r)
	if _, err := s.db.Exec(`DELETE FROM liaison_db_connections WHERE id=$1`, id); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, nil)
}

// handleDBQuery 执行 SQL 查询，返回列 + 行。
func (s *Service) handleDBQuery(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "db:*", ActExec) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	var req struct {
		ConnectionID int64  `json:"connection_id"`
		SQL          string `json:"sql"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.SQL == "" {
		httpx.Fail(w, http.StatusBadRequest, "connection_id/sql 必填")
		return
	}
	conn, err := s.dbReg.get(req.ConnectionID)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	start := time.Now()
	rows, err := conn.Query(req.SQL)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	// 限制结果集规模，防内存耗尽
	const maxRows = 1000
	out := make([][]any, 0, 32)
	n := 0
	for rows.Next() {
		if n >= maxRows {
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			httpx.Fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		for i, v := range vals {
			vals[i] = normDBValue(v)
		}
		out = append(out, vals)
		n++
	}
	httpx.OK(w, map[string]any{
		"columns": cols, "rows": out, "truncated": n >= maxRows,
		"elapsed_ms": time.Since(start).Milliseconds(),
	})
}

// get 获取（或建立）指定连接。
func (r *dbRegistry) get(id int64) (*sql.DB, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[id]; ok {
		return c, nil
	}
	var name, driver, dsn string
	if err := r.db.QueryRow(`SELECT name, driver, dsn FROM liaison_db_connections WHERE id=$1`, id).
		Scan(&name, &driver, &dsn); err != nil {
		return nil, fmt.Errorf("连接不存在: %w", err)
	}
	conn, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(2)
	conn.SetConnMaxLifetime(5 * time.Minute)
	r.cache[id] = conn
	return conn, nil
}

// normDBValue 把数据库值归一化为 JSON 友好的类型（时间 → RFC3339 字符串，[]byte → string）。
func normDBValue(v any) any {
	switch x := v.(type) {
	case time.Time:
		return x.Format(time.RFC3339)
	case []byte:
		return string(x)
	case nil:
		return nil
	default:
		return x
	}
}

// maskDSN 脱敏 DSN 中的密码（password=xxx / :pass@）。
func maskDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	// 简单脱敏：password= 与 :pass@ 形式
	if i := indexFold(dsn, "password="); i >= 0 {
		head := dsn[:i+len("password=")]
		rest := dsn[i+len("password="):]
		j := 0
		for j < len(rest) && rest[j] != ' ' && rest[j] != '&' {
			j++
		}
		return head + "***" + rest[j:]
	}
	return dsn
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// pathID 从路径参数 {id} 解析 int64。
func pathID(r *http.Request) int64 {
	var id int64
	_, _ = fmt.Sscanf(r.PathValue("id"), "%d", &id)
	return id
}

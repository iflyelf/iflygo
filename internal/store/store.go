// Package store 提供跨 PostgreSQL / SQLite 的统一数据访问层。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	"github.com/iflyelf/iflygo/internal/config"
	_ "github.com/lib/pq"  // PostgreSQL 驱动
	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动（CGO_ENABLED=0 可用）
)

// Dialect 数据库方言。
type Dialect string

const (
	Postgres Dialect = "postgres"
	SQLite   Dialect = "sqlite"
)

// Store 封装 *sql.DB 与方言相关能力。
type Store struct {
	DB      *sql.DB
	Dialect Dialect
}

// Open 依据配置打开数据库，并执行迁移。
func Open(c *config.Config) (*Store, error) {
	driver, dsn, err := c.DriverDSN()
	if err != nil {
		return nil, err
	}

	d := Postgres
	if driver == "sqlite" {
		d = SQLite
		dsn = sqliteDSN(dsn)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}

	maxOpen, maxIdle, lifetime := c.Database.PoolSettings()
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(lifetime)

	// 单机 SQLite：限制写并发，避免 "database is locked"
	if d == SQLite {
		db.SetMaxOpenConns(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	s := &Store{DB: db, Dialect: d}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}
	return s, nil
}

func sqliteDSN(path string) string {
	// WAL + busy_timeout + 外键约束，适合多进程只读/单进程写场景。
	return fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
}

var dollarRe = regexp.MustCompile(`\$([0-9]+)`)

// Rebind 将 $N 占位符转换为方言所需的占位符。
func (s *Store) Rebind(q string) string {
	if s.Dialect == Postgres {
		return q
	}
	return dollarRe.ReplaceAllString(q, "?")
}

// Now 返回数据库可用的当前时间占位（用参数传入，避免方言差异）。
func (s *Store) Now() time.Time { return time.Now() }

// Close 关闭数据库。
func (s *Store) Close() error { return s.DB.Close() }

// DriverName 返回当前驱动名。
func (s *Store) DriverName() string { return string(s.Dialect) }

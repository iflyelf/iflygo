package store

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// 每种方言一套迁移，按文件名 NNN_name.sql 排序执行。
//
//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// migrate 执行尚未应用的迁移。
func (s *Store) migrate() error {
	if _, err := s.DB.Exec(s.schemaMigrationsDDL()); err != nil {
		return fmt.Errorf("创建 schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := s.DB.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	migs, err := s.loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(s.Rebind(m.sql)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("迁移 %03d_%s 失败: %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(s.Rebind("INSERT INTO schema_migrations (version, name) VALUES ($1, $2)"), m.version, m.name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("记录迁移 %03d 失败: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) schemaMigrationsDDL() string {
	if s.Dialect == Postgres {
		return `CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`
	}
	return `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`
}

func (s *Store) loadMigrations() ([]migration, error) {
	fs := sqliteMigrations
	dir := "migrations/sqlite"
	if s.Dialect == Postgres {
		fs = postgresMigrations
		dir = "migrations/postgres"
	}
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("迁移文件名非法: %s", e.Name())
		}
		b, err := fs.ReadFile(dir + "/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: version, name: e.Name(), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

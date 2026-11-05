// Package migrate 数据库自动迁移系统。
//
// 设计：启动时自动检测当前 schema 版本，应用所有待执行的迁移脚本，
// 支持向前迁移（up）和回滚（down）。
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"
)

var dollarRe = regexp.MustCompile(`\$[0-9]+`)

//go:embed migrations/*.sql
var postgresMigrationsFS embed.FS

//go:embed migrations_sqlite/*.sql
var sqliteMigrationsFS embed.FS

// 方言常量。
const (
	DialectPostgres = "postgres"
	DialectSQLite   = "sqlite"
)

// Migration 单个迁移脚本。
type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

// Migrator 迁移管理器。
type Migrator struct {
	db       *sql.DB
	dialect  string
	migrations []Migration
}

// New 创建迁移管理器。
func New(db *sql.DB, dialect string) (*Migrator, error) {
	m := &Migrator{db: db, dialect: dialect}
	if err := m.loadMigrations(); err != nil {
		return nil, err
	}
	return m, nil
}

// loadMigrations 从 embed.FS 加载所有迁移脚本（按方言选择目录）。
func (m *Migrator) loadMigrations() error {
	fs := postgresMigrationsFS
	dir := "migrations"
	if m.dialect == DialectSQLite {
		fs = sqliteMigrationsFS
		dir = "migrations_sqlite"
	}
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取迁移目录失败: %w", err)
	}

	migrationMap := make(map[int]*Migration)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		// 解析文件名：001_init.up.sql / 001_init.down.sql
		name := entry.Name()
		var version int
		var migName string
		var direction string

		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			continue
		}

		if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil {
			continue
		}

		remaining := strings.TrimSuffix(parts[1], ".sql")
		if strings.HasSuffix(remaining, ".up") {
			direction = "up"
			migName = strings.TrimSuffix(remaining, ".up")
		} else if strings.HasSuffix(remaining, ".down") {
			direction = "down"
			migName = strings.TrimSuffix(remaining, ".down")
		} else {
			// 无后缀默认 up
			direction = "up"
			migName = remaining
		}

		content, err := fs.ReadFile(dir + "/" + name)
		if err != nil {
			return fmt.Errorf("读取迁移文件 %s 失败: %w", name, err)
		}

		if migrationMap[version] == nil {
			migrationMap[version] = &Migration{
				Version: version,
				Name:    migName,
			}
		}

		if direction == "up" {
			migrationMap[version].UpSQL = string(content)
		} else {
			migrationMap[version].DownSQL = string(content)
		}
	}

	// 排序
	versions := make([]int, 0, len(migrationMap))
	for v := range migrationMap {
		versions = append(versions, v)
	}
	sort.Ints(versions)

	for _, v := range versions {
		m.migrations = append(m.migrations, *migrationMap[v])
	}

	return nil
}

// querier 是 *sql.DB 与 *sql.Conn 的公共子集。
// Up 在持有 advisory lock 的同一条连接上完成全部操作，
// 避免 MaxOpenConns=1 时持锁连接占满连接池导致后续语句永久阻塞（死锁）。
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

var (
	_ querier = (*sql.DB)(nil)
	_ querier = (*sql.Conn)(nil)
)

// migrateLockKey 迁移专用 advisory lock 键，保证多实例并发启动时仅一个执行迁移，
// 其余实例阻塞等待，待迁移完成后拿到锁再 double-check 版本、自动跳过。零人工干预。
const migrateLockKey int64 = 0x7570677261646501 // "upgrade\x01"

// Up 执行所有待应用的迁移（自动创建 schema_migrations 表）。
//
// 多实例安全：全程持有 PG advisory lock（session 级），
// 并发启动的其它实例会阻塞在加锁处，直至本次迁移提交后自动获得锁并判定「已最新」。
func (m *Migrator) Up(ctx context.Context) error {
	// 0. 获取迁移锁（阻塞式）：跨实例串行化，拿不到就等待
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取迁移锁连接失败: %w", err)
	}
	defer conn.Close()
	useAdvisoryLock := true
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrateLockKey); err != nil {
		if strings.Contains(err.Error(), "no such function") || strings.Contains(err.Error(), "pg_advisory_lock") {
			// SQLite 无 advisory lock：退化为单写者（WAL + busy_timeout 保证安全）
			useAdvisoryLock = false
		} else {
			return fmt.Errorf("获取迁移锁失败: %w", err)
		}
	}
	if useAdvisoryLock {
		defer func() {
			_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrateLockKey)
		}()
	}

	// 1. 确保 schema_migrations 表存在（以下全部复用持锁连接 conn）
	if err := ensureMigrationsTable(ctx, conn, m.dialect); err != nil {
		return err
	}

	// 2. 获取当前版本（拿到锁后再读，保证看到其它实例已提交的最新版本）
	currentVersion, err := getCurrentVersion(ctx, conn)
	if err != nil {
		return err
	}

	log.Printf("📊 当前数据库版本: %d", currentVersion)

	// 3. 应用所有高于当前版本的迁移
	appliedCount := 0
	for _, mig := range m.migrations {
		if mig.Version <= currentVersion {
			continue
		}

		if mig.UpSQL == "" {
			log.Printf("⚠️ 迁移 %d (%s) 缺少 up 脚本，跳过", mig.Version, mig.Name)
			continue
		}

		log.Printf("🔄 应用迁移 %d: %s", mig.Version, mig.Name)

		if err := applyMigration(ctx, conn, mig, m.dialect); err != nil {
			return fmt.Errorf("应用迁移 %d 失败: %w", mig.Version, err)
		}

		appliedCount++
	}

	if appliedCount == 0 {
		log.Println("✅ 数据库已是最新版本")
	} else {
		log.Printf("✅ 成功应用 %d 个迁移", appliedCount)
	}

	return nil
}

// ensureMigrationsTable 确保 schema_migrations 表存在。
func ensureMigrationsTable(ctx context.Context, q querier, dialect string) error {
	ddl := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			applied_at TIMESTAMPTZ DEFAULT NOW()
		)
	`
	if dialect == DialectSQLite {
		ddl = `
			CREATE TABLE IF NOT EXISTS schema_migrations (
				version INTEGER PRIMARY KEY,
				name TEXT NOT NULL,
				applied_at TEXT DEFAULT CURRENT_TIMESTAMP
			)
		`
	}
	_, err := q.ExecContext(ctx, ddl)
	return err
}

// getCurrentVersion 获取当前数据库版本。
func getCurrentVersion(ctx context.Context, q querier) (int, error) {
	var version int
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) FROM schema_migrations
	`).Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

// applyMigration 应用单个迁移（事务内执行，按方言分句执行 + 占位符改写）。
func applyMigration(ctx context.Context, q querier, mig Migration, dialect string) error {
	tx, err := q.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 分句执行（SQLite 驱动不保证多语句 Exec 可靠；对「列已存在」幂等容错）
	for _, stmt := range splitStatements(mig.UpSQL) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			if dialect == DialectSQLite && isIdempotentErr(err) {
				continue
			}
			return fmt.Errorf("执行 SQL 失败: %w", err)
		}
	}

	// 记录版本
	_, err = tx.ExecContext(ctx, rebind(dialect, `
		INSERT INTO schema_migrations (version, name, applied_at)
		VALUES ($1, $2, $3)
	`), mig.Version, mig.Name, time.Now())
	if err != nil {
		return fmt.Errorf("记录版本失败: %w", err)
	}

	return tx.Commit()
}

// splitStatements 按分号切分 SQL 语句，过滤空行与注释。
func splitStatements(sql string) []string {
	var out []string
	for _, s := range strings.Split(sql, ";") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		// 去掉纯注释片段
		if onlyComments(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func onlyComments(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "--") {
			return false
		}
	}
	return true
}

// isIdempotentErr 判断 SQLite 下可安全跳过的幂等错误。
func isIdempotentErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, sub := range []string{"duplicate column name", "already exists", "duplicate index"} {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// rebind 将 $N 占位符改写为 ?（SQLite）。
func rebind(dialect, query string) string {
	if dialect != DialectSQLite {
		return query
	}
	return dollarRe.ReplaceAllString(query, "?")
}

// Down 回滚最后一个迁移。
func (m *Migrator) Down(ctx context.Context) error {
	currentVersion, err := getCurrentVersion(ctx, m.db)
	if err != nil {
		return err
	}

	if currentVersion == 0 {
		return fmt.Errorf("数据库已在初始版本，无法回滚")
	}

	// 找到对应版本的迁移
	var targetMig *Migration
	for i := range m.migrations {
		if m.migrations[i].Version == currentVersion {
			targetMig = &m.migrations[i]
			break
		}
	}

	if targetMig == nil {
		return fmt.Errorf("未找到版本 %d 的迁移脚本", currentVersion)
	}

	if targetMig.DownSQL == "" {
		return fmt.Errorf("迁移 %d 缺少 down 脚本", currentVersion)
	}

	log.Printf("🔙 回滚迁移 %d: %s", targetMig.Version, targetMig.Name)

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 执行回滚 SQL
	if _, err := tx.ExecContext(ctx, targetMig.DownSQL); err != nil {
		return fmt.Errorf("执行回滚 SQL 失败: %w", err)
	}

	// 删除版本记录
	_, err = tx.ExecContext(ctx, rebind(m.dialect, `DELETE FROM schema_migrations WHERE version = $1`), currentVersion)
	if err != nil {
		return fmt.Errorf("删除版本记录失败: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Printf("✅ 成功回滚到版本 %d", currentVersion-1)
	return nil
}

// Status 显示当前状态。
func (m *Migrator) Status(ctx context.Context) error {
	currentVersion, err := getCurrentVersion(ctx, m.db)
	if err != nil {
		return err
	}

	log.Printf("当前版本: %d", currentVersion)
	log.Println("可用迁移:")

	for _, mig := range m.migrations {
		status := "  "
		if mig.Version <= currentVersion {
			status = "✅"
		} else {
			status = "⏳"
		}
		log.Printf("%s %03d: %s", status, mig.Version, mig.Name)
	}

	return nil
}

// AutoMigrate 启动时自动迁移（推荐在 main 中调用）。
func AutoMigrate(db *sql.DB, dialect string) error {
	m, err := New(db, dialect)
	if err != nil {
		return err
	}
	return m.Up(context.Background())
}

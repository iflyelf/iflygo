// Package store 提供跨 PostgreSQL / SQLite 的统一数据访问层。
package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/config"
	"github.com/lib/pq"         // PostgreSQL 驱动 + 数组类型
	sqlite "modernc.org/sqlite" // 纯 Go SQLite 驱动（CGO_ENABLED=0 可用）
)

// Dialect 数据库方言。
type Dialect string

const (
	Postgres Dialect = "postgres"
	SQLite   Dialect = "sqlite"
)

// activeDialect 进程级当前方言。单进程只连一种库，Open 时设置一次（启动期，无并发写）。
var activeDialect = Postgres

// CurrentDialect 返回进程级当前方言。
func CurrentDialect() Dialect { return activeDialect }

// SQL 将 SQL 适配为当前进程方言（PostgreSQL 原样，SQLite 转换语法）。
// 供未持有 *Store 引用、但需要跨库运行 SQL 的包使用。
func SQL(q string) string { return AdaptSQL(activeDialect, q) }

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
		registerSQLiteFuncs()
	}
	activeDialect = d

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
	// _time_format=datetime：绑定 time.Time 时写 "2006-01-02 15:04:05"，与 CURRENT_TIMESTAMP/now() 一致。
	return fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_time_format=datetime", path)
}

// registerSQLiteFuncs 为 SQLite 注册 PostgreSQL 兼容函数，使业务 SQL 无需改写即可双库运行。
func registerSQLiteFuncs() {
	// now()：返回当前 UTC 时间（与 CURRENT_TIMESTAMP 同格式）
	sqlite.MustRegisterScalarFunction("now", 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		return time.Now().UTC().Format("2006-01-02 15:04:05"), nil
	})
	// pg_try_advisory_lock(x)：SQLite 单写者（WAL + busy_timeout），无并发抢占，恒返回 true
	sqlite.MustRegisterScalarFunction("pg_try_advisory_lock", 1, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		return true, nil
	})
	// pg_advisory_lock(x)：no-op（单写者天然串行）
	sqlite.MustRegisterScalarFunction("pg_advisory_lock", 1, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		return nil, nil
	})
	// pg_advisory_unlock(x)：恒返回 true
	sqlite.MustRegisterScalarFunction("pg_advisory_unlock", 1, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		return true, nil
	})
}

var dollarRe = regexp.MustCompile(`\$([0-9]+)`)

// jsonExtractRe 匹配 expr->>'key'（JSON 取字段）。
var jsonExtractRe = regexp.MustCompile(`([a-zA-Z_.][a-zA-Z0-9_.]*)\s*->>\s*'([^']*)'`)

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

// IsPostgres 判断是否为 PostgreSQL。
func (s *Store) IsPostgres() bool { return s.Dialect == Postgres }

// AdaptSQL 将 SQL 中的 PostgreSQL 语法转换为 SQLite 等价；PostgreSQL 原样返回。
// 覆盖：FOR UPDATE [SKIP LOCKED]、NOW()±INTERVAL、GREATEST/LEAST。
func AdaptSQL(d Dialect, sql string) string {
	if d == Postgres {
		return sql
	}
	sql = strings.ReplaceAll(sql, "FOR UPDATE SKIP LOCKED", "")
	sql = strings.ReplaceAll(sql, "FOR UPDATE", "")
	re := regexp.MustCompile(`NOW\(\)\s*([+-])\s*INTERVAL\s*'([^']*)'`)
	sql = re.ReplaceAllString(sql, `datetime('now', '$1$2')`)
	sql = strings.ReplaceAll(sql, "GREATEST(", "MAX(")
	sql = strings.ReplaceAll(sql, "LEAST(", "MIN(")
	// jsonb 函数 → SQLite json1 等价
	sql = strings.ReplaceAll(sql, "jsonb_build_object(", "json_object(")
	// -> > 'key' → json_extract(expr, '$.key')
	sql = jsonExtractRe.ReplaceAllString(sql, `json_extract($1, '$.$2')`)
	// ARRAY['a','b',...]::text[] → '["a","b",...]'（必须先于 ::cast 去除）
	sql = arrayLitRe.ReplaceAllStringFunc(sql, func(m string) string {
		inner := arrayLitRe.FindStringSubmatch(m)[1]
		var vals []string
		for _, p := range arrayElemRe.FindAllStringSubmatch(inner, -1) {
			vals = append(vals, p[1])
		}
		b, _ := json.Marshal(vals)
		return "'" + string(b) + "'"
	})
	// 去除 ::type 类型转换（::jsonb/::text/::varchar/::int/::text[] 等）
	castRe := regexp.MustCompile(`::[a-zA-Z_][a-zA-Z0-9_]*(?:\[\])?`)
	sql = castRe.ReplaceAllString(sql, "")
	// ILIKE → LIKE（SQLite LIKE 对 ASCII 已不区分大小写）
	sql = strings.ReplaceAll(sql, " ILIKE ", " LIKE ")
	// TO_CHAR(expr, '格式') → expr（SQLite 时间戳已存 TEXT，无需格式化）
	sql = toCharRe.ReplaceAllString(sql, `$1`)
	// <lhs> = ANY(<rhs>) → <lhs> IN (SELECT value FROM json_each(<rhs>))
	sql = anyRe.ReplaceAllStringFunc(sql, func(m string) string {
		sub := anyRe.FindStringSubmatch(m)
		return sub[1] + " IN (SELECT value FROM json_each(" + sub[2] + "))"
	})
	// <lhs> != ALL(<rhs>) → <lhs> NOT IN (SELECT value FROM json_each(<rhs>))
	sql = allRe.ReplaceAllStringFunc(sql, func(m string) string {
		sub := allRe.FindStringSubmatch(m)
		return sub[1] + " NOT IN (SELECT value FROM json_each(" + sub[3] + "))"
	})
	// UPDATE <table> <alias> SET → UPDATE <table> AS <alias> SET（SQLite 要求 AS）
	sql = updateAliasRe.ReplaceAllString(sql, `UPDATE $1 AS $2 SET`)
	// DELETE FROM <table> <alias> → DELETE FROM <table> AS <alias>（SQLite 要求 AS）
	sql = deleteAliasRe.ReplaceAllString(sql, `DELETE FROM $1 AS $2`)
	return sql
}

var anyRe = regexp.MustCompile(`([A-Za-z0-9_.$]+)\s*=\s*ANY\s*\(\s*([A-Za-z0-9_.$]+)\s*\)`)
var allRe = regexp.MustCompile(`([A-Za-z0-9_.$]+)\s*(?:!=|<>)\s*ALL\s*\(\s*([A-Za-z0-9_.$]+)\s*\)`)
var updateAliasRe = regexp.MustCompile(`UPDATE\s+(\w+)\s+(\w+)\s+SET`)
var deleteAliasRe = regexp.MustCompile(`DELETE\s+FROM\s+(\w+)\s+(\w+)`)
var arrayLitRe = regexp.MustCompile(`ARRAY\[((?:'[^']*')(?:,\s*'[^']*')*)\]::text\[\]`)
var arrayElemRe = regexp.MustCompile(`'([^']*)'`)
var toCharRe = regexp.MustCompile(`TO_CHAR\(\s*([^,()]+)\s*,\s*'[^']*'\s*\)`)

// ============================================================
// 数组参数/扫描跨库抽象
// ============================================================

// Array 将切片包装为可同时作为 SQL 参数（driver.Valuer）与扫描目标（sql.Scanner）
// 的跨库数组值。PG 用 pq.Array，SQLite 用 JSON 文本。
// 支持 []string / []int64 / []float64 / []bool 及其指针。
func Array(vals any) any {
	if activeDialect == SQLite {
		return &jsonArray{ptr: vals}
	}
	return pq.Array(vals)
}

// ArrayInt64 将 []int64 包装为跨库数组值。
func ArrayInt64(vals []int64) any { return Array(vals) }

type jsonArray struct{ ptr any }

func (a *jsonArray) Value() (driver.Value, error) {
	v := reflect.Indirect(reflect.ValueOf(a.ptr))
	b, err := json.Marshal(v.Interface())
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (a *jsonArray) Scan(src any) error {
	if src == nil {
		return nil
	}
	raw, err := rawString(src)
	if err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), a.ptr)
}

// StringArray 可从 PostgreSQL(pq) 与 SQLite(JSON 文本) 扫描字符串数组。
type StringArray []string

func (a *StringArray) Scan(src any) error {
	if src == nil {
		*a = nil
		return nil
	}
	raw, err := rawString(src)
	if err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		var out []string
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return err
		}
		*a = out
		return nil
	}
	var psa pq.StringArray
	if err := psa.Scan([]byte(raw)); err != nil {
		return err
	}
	*a = StringArray(psa)
	return nil
}

func (a StringArray) Value() (driver.Value, error) {
	if activeDialect == SQLite {
		b, err := json.Marshal([]string(a))
		if err != nil {
			return nil, err
		}
		return string(b), nil
	}
	return pq.StringArray(a).Value()
}

// Int64Array 可从 PostgreSQL(pq) 与 SQLite(JSON 文本) 扫描整数数组。
type Int64Array []int64

func (a *Int64Array) Scan(src any) error {
	if src == nil {
		*a = nil
		return nil
	}
	raw, err := rawString(src)
	if err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		var out []int64
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return err
		}
		*a = out
		return nil
	}
	var pia pq.Int64Array
	if err := pia.Scan([]byte(raw)); err != nil {
		return err
	}
	*a = Int64Array(pia)
	return nil
}

func (a Int64Array) Value() (driver.Value, error) {
	if activeDialect == SQLite {
		b, err := json.Marshal([]int64(a))
		if err != nil {
			return nil, err
		}
		return string(b), nil
	}
	return pq.Int64Array(a).Value()
}

func rawString(src any) (string, error) {
	switch v := src.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return "", fmt.Errorf("unsupported array value type %T", src)
	}
}

// ============================================================
// 时间戳扫描跨库抽象（SQLite 的 TEXT 时间戳无法直接扫入 time.Time）
// ============================================================

var timeFormats = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	"2006-01-02",
	time.RFC3339Nano,
}

func parseTimeValue(src any) (time.Time, error) {
	switch v := src.(type) {
	case time.Time:
		return v, nil
	case string:
		return parseTimeString(v)
	case []byte:
		return parseTimeString(string(v))
	default:
		return time.Time{}, fmt.Errorf("unsupported time value type %T", src)
	}
}

func parseTimeString(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	for _, f := range timeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间戳 %q", s)
}

// Time 可同时从 PostgreSQL(time.Time) 与 SQLite(TEXT) 扫描时间戳，内嵌 time.Time。
type Time struct{ time.Time }

func (t *Time) Scan(src any) error {
	tt, err := parseTimeValue(src)
	if err != nil {
		return err
	}
	t.Time = tt
	return nil
}

func (t Time) Value() (driver.Value, error) { return t.Time, nil }

// NullTime 等价于 sql.NullTime，但可从 SQLite TEXT 时间戳扫描。
type NullTime struct {
	Time  time.Time
	Valid bool
}

func (n *NullTime) Scan(src any) error {
	if src == nil {
		n.Time, n.Valid = time.Time{}, false
		return nil
	}
	tt, err := parseTimeValue(src)
	if err != nil {
		return err
	}
	n.Time, n.Valid = tt, true
	return nil
}

func (n NullTime) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Time, nil
}

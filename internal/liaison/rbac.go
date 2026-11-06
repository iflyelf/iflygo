// Package liaison 远程管理套件：Web SSH / RDP / VNC / 文件管理 / Web DB / LLM 网关 / Casbin RBAC。
//
// 统一通过一个网关入口（/api/liaison/*）提供服务，所有子功能共享：
//   - Casbin RBAC 授权（策略存库，双库：PostgreSQL / SQLite）
//   - 会话鉴权（复用 upgrade_mgr 的 auth 会话 Cookie）
//   - WebSocket 通道（SSH/RDP/VNC 终端的双向流）
package liaison

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/iflyelf/iflygo/internal/store"
)

// casbinModel 内联 RBAC 模型（含 role 继承 g）。
// 匹配器：subject 直接相等或经 g 角色继承；对象用 keyMatch（* 前缀通配，对冒号不敏感）；动作精确或 * 通配。
const casbinModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = (r.sub == p.sub || g(r.sub, p.sub)) && keyMatch(r.obj, p.obj) && (p.act == "*" || r.act == p.act)
`

// 资源对象前缀（obj 格式：<kind>:<name>，如 ssh:10.77.0.2、files:nas、db:main、llm:chat、desktop:win）。
const (
	ObjSSH     = "ssh"
	ObjDesktop = "desktop" // RDP/VNC
	ObjFiles   = "files"
	ObjDB      = "db"
	ObjLLM     = "llm"
)

// 动作（act）。
const (
	ActConnect = "connect" // 建立会话（SSH/RDP/VNC）
	ActRead    = "read"
	ActWrite   = "write"
	ActExec    = "exec" // 执行命令/查询
	ActAdmin   = "admin"
)

// policyAdapter 把 Casbin 策略持久化到双库（liaison_policies 表）。
type policyAdapter struct {
	db   *sql.DB
	once sync.Once
}

// NewPolicyAdapter 构造策略适配器（首次 Enforce 前自动建表）。
func NewPolicyAdapter(db *sql.DB) *policyAdapter { return &policyAdapter{db: db} }

// ensureTable 建策略表（幂等；SQLite 与 PG 通用语法）。
func (a *policyAdapter) ensureTable() {
	a.once.Do(func() {
		_, _ = a.db.Exec(store.SQL(`
			CREATE TABLE IF NOT EXISTS liaison_policies (
				id    BIGSERIAL PRIMARY KEY,
				ptype VARCHAR(10)  NOT NULL DEFAULT '',
				v0    VARCHAR(255) NOT NULL DEFAULT '',
				v1    VARCHAR(255) NOT NULL DEFAULT '',
				v2    VARCHAR(255) NOT NULL DEFAULT '',
				v3    VARCHAR(255) NOT NULL DEFAULT ''
			)`))
	})
}

// loadPolicyLine 读取一行策略，按列数返回字符串切片。
// （占位，实际加载逻辑在 LoadPolicy 中直接实现）

// LoadPolicy 从库加载全部策略（Casbin 标准接口）。
func (a *policyAdapter) LoadPolicy(m model.Model) error {
	a.ensureTable()
	rows, err := a.db.Query(`SELECT ptype, v0, v1, v2, v3 FROM liaison_policies`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ptype, v0, v1, v2, v3 string
		if err := rows.Scan(&ptype, &v0, &v1, &v2, &v3); err != nil {
			return err
		}
		line := []string{}
		for _, v := range []string{v0, v1, v2, v3} {
			if v != "" {
				line = append(line, v)
			}
		}
		if len(line) == 0 {
			continue
		}
		persistLine(m, ptype, line)
	}
	return rows.Err()
}

// persistLine 将一行策略写入 model（Casbin 内部无导出接口，这里直接操作 model）。
func persistLine(m model.Model, ptype string, line []string) {
	ast, ok := m["p"][ptype]
	if !ok {
		ast, ok = m["g"][ptype]
		if !ok {
			return
		}
	}
	ast.Policy = append(ast.Policy, line)
}

// SavePolicy 全量保存策略（删除后重建；Casbin 标准接口）。
func (a *policyAdapter) SavePolicy(m model.Model) error {
	a.ensureTable()
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM liaison_policies`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO liaison_policies (ptype, v0, v1, v2, v3) VALUES ($1,$2,$3,$4,$5)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for ptype, ast := range m["p"] {
		for _, rule := range ast.Policy {
			if _, err := stmt.Exec(ptype, at(rule, 0), at(rule, 1), at(rule, 2), at(rule, 3)); err != nil {
				return err
			}
		}
	}
	for ptype, ast := range m["g"] {
		for _, rule := range ast.Policy {
			if _, err := stmt.Exec(ptype, at(rule, 0), at(rule, 1), at(rule, 2), at(rule, 3)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// AddPolicy 追加一条策略。
func (a *policyAdapter) AddPolicy(sec, ptype string, rule []string) error {
	a.ensureTable()
	_, err := a.db.Exec(
		`INSERT INTO liaison_policies (ptype, v0, v1, v2, v3) VALUES ($1,$2,$3,$4,$5)`,
		ptype, at(rule, 0), at(rule, 1), at(rule, 2), at(rule, 3))
	return err
}

// RemovePolicy 删除一条策略。
func (a *policyAdapter) RemovePolicy(sec, ptype string, rule []string) error {
	a.ensureTable()
	_, err := a.db.Exec(
		`DELETE FROM liaison_policies WHERE ptype=$1 AND v0=$2 AND v1=$3 AND v2=$4 AND v3=$5`,
		ptype, at(rule, 0), at(rule, 1), at(rule, 2), at(rule, 3))
	return err
}

// RemoveFilteredPolicy 按字段前缀删除策略。
func (a *policyAdapter) RemoveFilteredPolicy(sec, ptype string, fieldIndex int, fieldValues ...string) error {
	a.ensureTable()
	cols := []string{"v0", "v1", "v2", "v3"}
	var sb strings.Builder
	sb.WriteString(`DELETE FROM liaison_policies WHERE ptype=$1`)
	args := []any{ptype}
	for i := 0; i < len(fieldValues); i++ {
		if fieldValues[i] != "" {
			sb.WriteString(fmt.Sprintf(" AND %s=$%d", cols[fieldIndex+i], len(args)+1))
			args = append(args, fieldValues[i])
		}
	}
	_, err := a.db.Exec(sb.String(), args...)
	return err
}

func at(rule []string, i int) string {
	if i < len(rule) {
		return rule[i]
	}
	return ""
}

// RBAC 包装 Casbin Enforcer，提供面向业务的鉴权接口。
type RBAC struct {
	enforcer *casbin.Enforcer
}

// NewRBAC 构造 RBAC（自动建表 + 加载策略 + 确保默认策略）。
func NewRBAC(db *sql.DB) (*RBAC, error) {
	m, err := model.NewModelFromString(casbinModel)
	if err != nil {
		return nil, fmt.Errorf("解析 Casbin 模型失败: %w", err)
	}
	adapter := NewPolicyAdapter(db)
	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("初始化 Casbin 失败: %w", err)
	}
	r := &RBAC{enforcer: e}
	if err := r.ensureDefaults(); err != nil {
		return nil, err
	}
	return r, nil
}

// ensureDefaults 若尚无任何 p 策略，写入默认策略（superadmin/admin 全权限）。
func (r *RBAC) ensureDefaults() error {
	pols, err := r.enforcer.GetPolicy()
	if err != nil {
		return err
	}
	if len(pols) > 0 {
		return nil
	}
	for _, p := range DefaultPolicies() {
		if _, err := r.enforcer.AddPolicy(p); err != nil {
			return err
		}
	}
	return nil
}

// Allow 判断 subject（角色名）是否对 obj 执行 act（Enforce 出错默认拒绝）。
func (r *RBAC) Allow(sub, obj, act string) bool {
	ok, err := r.enforcer.Enforce(sub, obj, act)
	if err != nil {
		return false
	}
	return ok
}

// AllowRoles 判断用户（含多角色）是否对 obj 执行 act：任一角色命中即放行。
func (r *RBAC) AllowRoles(roles []string, obj, act string) bool {
	if len(roles) == 0 {
		return false
	}
	for _, role := range roles {
		if r.Allow(role, obj, act) {
			return true
		}
	}
	return false
}

// AddPolicy 添加策略。
func (r *RBAC) AddPolicy(sub, obj, act string) error {
	_, err := r.enforcer.AddPolicy(sub, obj, act)
	return err
}

// AddRoleForUser 给用户授予角色（g 策略）。
func (r *RBAC) AddRoleForUser(user, role string) error {
	_, err := r.enforcer.AddGroupingPolicy(user, role)
	return err
}

// DefaultPolicies 返回默认策略：superadmin/admin 全权限，其余按用户显式授权。
// obj 使用通配符 * 表示全部资源。
func DefaultPolicies() [][]string {
	return [][]string{
		{"superadmin", "*", "*"},
		{"admin", "*", "*"},
	}
}

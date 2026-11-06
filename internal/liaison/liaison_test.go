package liaison

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestRBAC 用 SQLite 内存库构造 RBAC。
func newTestRBAC(t *testing.T) *RBAC {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	r, err := NewRBAC(db)
	if err != nil {
		t.Fatalf("NewRBAC: %v", err)
	}
	return r
}

func TestDefaultPolicies(t *testing.T) {
	r := newTestRBAC(t)
	// superadmin 全权限
	if !r.Allow("superadmin", "ssh:10.0.0.1", "connect") {
		t.Error("superadmin 应能连接任意主机")
	}
	if !r.Allow("superadmin", "db:*", "exec") {
		t.Error("superadmin 应能执行任意 DB 查询")
	}
	// admin 全权限
	if !r.Allow("admin", "files:*", "read") {
		t.Error("admin 应能读取任意文件")
	}
	// 普通用户默认无权限
	if r.Allow("user", "ssh:10.0.0.1", "connect") {
		t.Error("普通用户默认不应有 SSH 权限")
	}
}

func TestAllowRoles(t *testing.T) {
	r := newTestRBAC(t)
	// 多角色：任一命中即放行
	if !r.AllowRoles([]string{"user", "admin"}, "ssh:10.0.0.1", "connect") {
		t.Error("含 admin 角色应放行")
	}
	if r.AllowRoles([]string{"user"}, "ssh:10.0.0.1", "connect") {
		t.Error("仅 user 角色应拒绝")
	}
	if r.AllowRoles(nil, "ssh:10.0.0.1", "connect") {
		t.Error("空角色应拒绝")
	}
}

func TestAddPolicy(t *testing.T) {
	r := newTestRBAC(t)
	// 给普通用户显式授权 ssh:10.0.0.1 的 connect
	if err := r.AddPolicy("user", "ssh:10.0.0.1", "connect"); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	if !r.Allow("user", "ssh:10.0.0.1", "connect") {
		t.Error("显式授权后应放行")
	}
	if r.Allow("user", "ssh:10.0.0.2", "connect") {
		t.Error("未授权的主机应拒绝")
	}
	if r.Allow("user", "ssh:10.0.0.1", "exec") {
		t.Error("动作不匹配应拒绝")
	}
}

func TestSafePath(t *testing.T) {
	cases := []struct {
		in   string
		want string // 空表示期望错误
	}{
		{"/tmp", "/tmp"},
		{"/etc/passwd", "/etc/passwd"},
		{"tmp/foo", "/tmp/foo"},
		{"/", "/"},
	}
	for _, c := range cases {
		got, err := safePath(c.in)
		if err != nil {
			t.Errorf("safePath(%q) 意外错误: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("safePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSSHConfig(t *testing.T) {
	cfg, err := sshConfig(wsHandshake{User: "root", Password: "secret"})
	if err != nil {
		t.Fatalf("密码凭据应解析成功: %v", err)
	}
	if cfg.User != "root" || len(cfg.Auth) != 1 {
		t.Errorf("SSH 配置不正确: user=%q auth=%d", cfg.User, len(cfg.Auth))
	}

	if _, err := sshConfig(wsHandshake{User: "root"}); err == nil {
		t.Error("无凭据应报错")
	}
}

func TestItoa(t *testing.T) {
	if itoa(0) != "0" || itoa(22) != "22" || itoa(5000) != "5000" {
		t.Errorf("itoa 结果错误: %q %q %q", itoa(0), itoa(22), itoa(5000))
	}
}

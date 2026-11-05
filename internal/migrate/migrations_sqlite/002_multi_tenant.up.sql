-- 多租户 + RBAC + 同步日志（对齐权威 schema：业务表来自 001）

-- 1. 租户表
CREATE TABLE IF NOT EXISTS tenants (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT UNIQUE NOT NULL,
    display_name TEXT DEFAULT '',
    status       TEXT DEFAULT 'active',
    created_at   TEXT DEFAULT CURRENT_TIMESTAMP
);

-- 默认租户（必须在加外键前存在）
INSERT INTO tenants (id, name, display_name, status)
VALUES (1, 'default', '默认租户', 'active')
ON CONFLICT DO NOTHING;

-- 2. RBAC 用户表（与内置账号 local_users 分离：此表承载租户/角色映射）
CREATE TABLE IF NOT EXISTS users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    username   TEXT NOT NULL UNIQUE,
    tenant_id  INT NOT NULL DEFAULT 1 REFERENCES tenants(id),
    roles      TEXT NOT NULL DEFAULT '[\"user\"]',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);

-- 3. 用户字段同步日志（maintenance 清理引用）
CREATE TABLE IF NOT EXISTS sync_logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    source       TEXT DEFAULT '',
    status       TEXT DEFAULT '',
    total        INT DEFAULT 0,
    message      TEXT DEFAULT '',
    started_at   TEXT DEFAULT CURRENT_TIMESTAMP,
    completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_sync_logs_started ON sync_logs(started_at DESC);

-- 4. 业务表加 tenant_id（幂等）+ 默认租户 + 外键
ALTER TABLE hosts ADD COLUMN tenant_id INT;
ALTER TABLE components ADD COLUMN tenant_id INT;
ALTER TABLE artifacts ADD COLUMN tenant_id INT;
ALTER TABLE upgrade_tasks ADD COLUMN tenant_id INT;

UPDATE hosts         SET tenant_id = 1 WHERE tenant_id IS NULL;
UPDATE components    SET tenant_id = 1 WHERE tenant_id IS NULL;
UPDATE artifacts     SET tenant_id = 1 WHERE tenant_id IS NULL;
UPDATE upgrade_tasks SET tenant_id = 1 WHERE tenant_id IS NULL;


-- 外键（用 DO 块保证幂等：约束已存在则跳过）


CREATE INDEX IF NOT EXISTS idx_hosts_tenant      ON hosts(tenant_id);
CREATE INDEX IF NOT EXISTS idx_components_tenant ON components(tenant_id);
CREATE INDEX IF NOT EXISTS idx_artifacts_tenant  ON artifacts(tenant_id);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant      ON upgrade_tasks(tenant_id);

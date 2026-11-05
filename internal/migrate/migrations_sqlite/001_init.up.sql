-- upgrade_mgr 数据库结构（幂等，可重复执行）
-- 由服务端启动时自动执行（internal/svc/schema.go），此文件为参考快照。

-- ============================================================
-- 应用设置（页面可配置，DB 优先 / env 兜底）
-- ============================================================
CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- 内置账号（provider=builtin 时使用；casdoor 模式下不使用）
-- ============================================================
CREATE TABLE IF NOT EXISTS local_users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name  TEXT DEFAULT '',
    is_admin      INTEGER NOT NULL DEFAULT FALSE,
    disabled      INTEGER NOT NULL DEFAULT FALSE,
    created_at    TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at    TEXT DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- 主机清单（事实来源；Consul 只是同步来源之一）
-- ============================================================
CREATE TABLE IF NOT EXISTS hosts (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    ip                 TEXT NOT NULL UNIQUE,
    hostname           TEXT DEFAULT '',
    consul_node        TEXT DEFAULT '',
    datacenter         TEXT DEFAULT '',
    os_family          TEXT DEFAULT '',   -- centos/ubuntu/debian/...
    os_version         TEXT DEFAULT '',
    kernel             TEXT DEFAULT '',
    arch               TEXT DEFAULT '',   -- amd64/arm64/...
    -- 业务分组（优先取自 Consul ServiceMeta）
    account            TEXT DEFAULT '',
    app_code           TEXT DEFAULT '',
    biz_group          TEXT DEFAULT '',
    department         TEXT DEFAULT '',
    vendor             TEXT DEFAULT '',
    os_type            TEXT DEFAULT '',
    host_type          TEXT DEFAULT '',
    tags               TEXT DEFAULT '{}',
    -- Agent 与保活
    agent_id           TEXT DEFAULT '',
    agent_version      TEXT DEFAULT '',
    agent_status       TEXT DEFAULT 'unknown', -- online/upgrading/restarting/crash_loop/offline/unknown
    supervisor         TEXT DEFAULT '',        -- systemd/upstart/cron/bare
    component_versions TEXT DEFAULT '{}',
    -- 维护窗口
    maintenance_until  TEXT,
    maintenance_reason TEXT DEFAULT '',
    last_stage         TEXT DEFAULT '',
    -- 来源与心跳
    source             TEXT NOT NULL DEFAULT 'consul', -- agent/manual/csv/consul
    last_seen          TEXT,
    last_synced_at     TEXT,
    created_at         TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at         TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_hosts_app_code   ON hosts(app_code);
CREATE INDEX IF NOT EXISTS idx_hosts_biz_group  ON hosts(biz_group);
CREATE INDEX IF NOT EXISTS idx_hosts_os_family  ON hosts(os_family);
CREATE INDEX IF NOT EXISTS idx_hosts_arch       ON hosts(arch);
CREATE INDEX IF NOT EXISTS idx_hosts_agent_stat ON hosts(agent_status);
CREATE INDEX IF NOT EXISTS idx_hosts_source     ON hosts(source);

-- ============================================================
-- 组件定义
-- ============================================================
CREATE TABLE IF NOT EXISTS components (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL UNIQUE,
    display_name   TEXT DEFAULT '',
    deploy_mode    TEXT NOT NULL DEFAULT 'replace', -- replace/exec/replace_exec/script
    binary_name    TEXT DEFAULT '',
    install_path   TEXT DEFAULT '',
    backup_dir     TEXT DEFAULT '/var/lib/upgrade-agent/backup',
    exec_path      TEXT DEFAULT '',
    exec_args      TEXT DEFAULT '{}',
    exec_as_user   TEXT DEFAULT '',
    exec_timeout   TEXT DEFAULT '10m',
    exec_env       TEXT DEFAULT '{}',
    work_dir       TEXT DEFAULT '',
    ignore_error   INTEGER NOT NULL DEFAULT FALSE,
    pre_install    TEXT DEFAULT '[]',   -- [][]string
    post_install   TEXT DEFAULT '[]',
    service_name   TEXT DEFAULT '',
    service_mgr    TEXT DEFAULT 'auto', -- auto/systemd/sysv/none
    stop_cmd       TEXT DEFAULT '[]',
    start_cmd      TEXT DEFAULT '[]',
    restart_cmd    TEXT DEFAULT '[]',
    version_cmd    TEXT DEFAULT '[]',
    version_regex  TEXT DEFAULT '',
    health_cmd     TEXT DEFAULT '[]',
    consul_maint   INTEGER NOT NULL DEFAULT FALSE,
    os_match       TEXT DEFAULT '',
    arch_match     TEXT DEFAULT '',
    enabled        INTEGER NOT NULL DEFAULT TRUE,
    created_at     TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at     TEXT DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- 制品（组件 × 版本 × OS × ARCH）
-- ============================================================
CREATE TABLE IF NOT EXISTS artifacts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    component_id    BIGINT NOT NULL REFERENCES components(id) ON DELETE CASCADE,
    version         TEXT NOT NULL,
    os_family       TEXT DEFAULT '',
    os_version_spec TEXT DEFAULT '',
    arch            TEXT NOT NULL,
    filename        TEXT NOT NULL,
    size            BIGINT NOT NULL DEFAULT 0,
    md5             TEXT NOT NULL,
    sha256          TEXT DEFAULT '',
    storage         TEXT NOT NULL DEFAULT 'local',
    path            TEXT NOT NULL,
    inner_path      TEXT DEFAULT '',
    notes           TEXT DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'active',
    uploaded_by     TEXT DEFAULT '',
    created_at      TEXT DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(component_id, version, arch, os_family)
);
CREATE INDEX IF NOT EXISTS idx_artifacts_component ON artifacts(component_id);

-- ============================================================
-- 升级任务
-- ============================================================
CREATE TABLE IF NOT EXISTS upgrade_tasks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    component_id BIGINT NOT NULL REFERENCES components(id) ON DELETE RESTRICT,
    version      TEXT NOT NULL,
    selector     TEXT NOT NULL DEFAULT '{}',
    strategy     TEXT NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'draft', -- draft/running/paused/success/failed/rolled_back/canceled
    schedule_at  TEXT,
    created_by   TEXT DEFAULT '',
    created_at   TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at   TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_tasks_status  ON upgrade_tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON upgrade_tasks(created_at DESC);

CREATE TABLE IF NOT EXISTS task_batches (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     BIGINT NOT NULL REFERENCES upgrade_tasks(id) ON DELETE CASCADE,
    batch_index INT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    total       INT NOT NULL DEFAULT 0,
    success     INT NOT NULL DEFAULT 0,
    failed      INT NOT NULL DEFAULT 0,
    started_at  TEXT,
    finished_at TEXT,
    UNIQUE(task_id, batch_index)
);

CREATE TABLE IF NOT EXISTS task_hosts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id      BIGINT NOT NULL REFERENCES upgrade_tasks(id) ON DELETE CASCADE,
    host_id      BIGINT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    batch_index  INT NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'pending', -- pending/running/success/failed/rolled_back/skipped/timeout
    stage        TEXT DEFAULT '',
    from_version TEXT DEFAULT '',
    to_version   TEXT DEFAULT '',
    attempts     INT NOT NULL DEFAULT 0,
    exit_code    INT DEFAULT 0,
    log          TEXT DEFAULT '',
    started_at   TEXT,
    finished_at  TEXT,
    UNIQUE(task_id, host_id)
);
CREATE INDEX IF NOT EXISTS idx_task_hosts_task   ON task_hosts(task_id);
CREATE INDEX IF NOT EXISTS idx_task_hosts_status ON task_hosts(status);

-- ============================================================
-- Agent 与指令队列
-- ============================================================
CREATE TABLE IF NOT EXISTS agents (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id          TEXT NOT NULL UNIQUE,
    host_id           BIGINT REFERENCES hosts(id) ON DELETE SET NULL,
    ip                TEXT DEFAULT '',
    token_hash        TEXT NOT NULL,
    secret            TEXT NOT NULL DEFAULT '', -- 命令信封 HMAC 密钥
    arch              TEXT DEFAULT '',
    os                TEXT DEFAULT '',
    agent_version     TEXT DEFAULT '',
    supervisor        TEXT DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'offline',
    restart_count     INT NOT NULL DEFAULT 0,
    last_restart_at   TEXT,
    last_heartbeat_at TEXT,
    created_at        TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_agents_host ON agents(host_id);

CREATE TABLE IF NOT EXISTS commands (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id             BIGINT REFERENCES upgrade_tasks(id) ON DELETE CASCADE,
    task_host_id        BIGINT REFERENCES task_hosts(id) ON DELETE CASCADE,
    host_id             BIGINT REFERENCES hosts(id) ON DELETE CASCADE,
    agent_id            TEXT NOT NULL,
    type                TEXT NOT NULL,
    payload             TEXT NOT NULL DEFAULT '{}',
    status              TEXT NOT NULL DEFAULT 'pending', -- pending/dispatched/accepted/running/success/failed/rolled_back/timeout/expired/canceled
    attempts            INT NOT NULL DEFAULT 0,
    maintenance_seconds INT NOT NULL DEFAULT 0,
    will_restart        INTEGER NOT NULL DEFAULT FALSE,
    lease_expire_at     TEXT,
    expire_at           TEXT,
    result              TEXT,
    log                 TEXT DEFAULT '',
    created_at          TEXT DEFAULT CURRENT_TIMESTAMP,
    dispatched_at       TEXT,
    started_at          TEXT,
    finished_at         TEXT
);
CREATE INDEX IF NOT EXISTS idx_commands_agent_status ON commands(agent_id, status);
CREATE INDEX IF NOT EXISTS idx_commands_task         ON commands(task_id);

-- ============================================================
-- 审计日志
-- ============================================================
CREATE TABLE IF NOT EXISTS audit_logs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT,
    username      TEXT,
    action        TEXT,
    resource_type TEXT,
    resource_id   TEXT,
    resource_name TEXT,
    details       TEXT,
    ip_address    TEXT,
    status        TEXT,
    error_message TEXT,
    created_at    TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_user    ON audit_logs(user_id);

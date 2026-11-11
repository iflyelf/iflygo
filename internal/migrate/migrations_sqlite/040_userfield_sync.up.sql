-- 用户字段定义自动同步（对齐 consul_mgr）：单例配置 + 历史日志。
CREATE TABLE IF NOT EXISTS userfield_sync_config (
    id              INTEGER PRIMARY KEY,
    enabled         INTEGER NOT NULL DEFAULT 0,
    interval        TEXT NOT NULL DEFAULT '6h',
    sync_on_startup INTEGER NOT NULL DEFAULT 0,
    last_run_at     TEXT,
    last_status     TEXT NOT NULL DEFAULT '',
    last_message    TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS userfield_sync_logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    status       TEXT NOT NULL DEFAULT 'running',
    trigger_type TEXT NOT NULL DEFAULT 'manual',
    total        INTEGER NOT NULL DEFAULT 0,
    added        INTEGER NOT NULL DEFAULT 0,
    updated      INTEGER NOT NULL DEFAULT 0,
    message      TEXT NOT NULL DEFAULT '',
    started_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_userfield_sync_logs_started ON userfield_sync_logs(started_at DESC);

INSERT INTO userfield_sync_config (id, enabled, interval, sync_on_startup)
VALUES (1, 0, '6h', 0)
ON CONFLICT (id) DO NOTHING;

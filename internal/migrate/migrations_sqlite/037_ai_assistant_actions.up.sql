-- AI 助手待确认操作：写工具不直接执行，先暂存，用户在页面确认后执行；过期由每日维护自动清理。
CREATE TABLE IF NOT EXISTS ai_assistant_actions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id   INT NOT NULL DEFAULT 1 REFERENCES tenants(id),
    username    TEXT NOT NULL,
    tool        TEXT NOT NULL,
    args        TEXT NOT NULL DEFAULT '{}',
    summary     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  TEXT NOT NULL,
    executed_at TEXT,
    executed_by TEXT NOT NULL DEFAULT '',
    result      TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_ai_assistant_actions_user
    ON ai_assistant_actions(username, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_assistant_actions_expires
    ON ai_assistant_actions(expires_at);


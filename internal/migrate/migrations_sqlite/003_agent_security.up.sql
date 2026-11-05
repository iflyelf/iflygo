-- Agent 安全加固：一次性注册令牌 + 指令 nonce 去重

-- 一次性注册令牌（enroll token）：由管理员生成，Agent 注册时校验并消费
CREATE TABLE IF NOT EXISTS enroll_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT NOT NULL UNIQUE,  -- sha256(token) 十六进制
    created_by TEXT DEFAULT '',
    note       TEXT DEFAULT '',
    expires_at TEXT NOT NULL,
    used_at    TEXT,                  -- 非空表示已消费
    used_by_ip TEXT DEFAULT '',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_enroll_tokens_exp ON enroll_tokens(expires_at);

-- 指令 nonce 去重：Agent 侧防重放的服务端配套（记录已下发 nonce，短期保留）
CREATE TABLE IF NOT EXISTS command_nonces (
    nonce      TEXT PRIMARY KEY,
    agent_id   TEXT NOT NULL,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_command_nonces_created ON command_nonces(created_at);

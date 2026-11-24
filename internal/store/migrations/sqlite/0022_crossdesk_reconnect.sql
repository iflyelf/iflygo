-- CrossDesk 重连凭据：设备 ID -> token 哈希，30 天有效。
CREATE TABLE IF NOT EXISTS crossdesk_reconnect_credentials (
    device_id  TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL,
    expires_at INTEGER NOT NULL DEFAULT 0
);

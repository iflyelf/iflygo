-- CrossDesk Web 一次性连接票据：管理员在 iFlyGo 生成，60 秒有效、单次使用。
CREATE TABLE IF NOT EXISTS crossdesk_join_tickets (
    token_hash TEXT PRIMARY KEY,
    device_id  TEXT NOT NULL,
    expires_at BIGINT NOT NULL DEFAULT 0,
    used_at    BIGINT NOT NULL DEFAULT 0
);

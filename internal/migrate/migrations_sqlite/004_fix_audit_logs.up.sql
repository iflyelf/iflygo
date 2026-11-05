-- 修复 audit_logs 结构漂移（幂等，向前兼容）
--
-- 背景：某中间版本曾以不同字段定义重复创建 audit_logs（ip/resource/success/message），
-- 与权威定义（迁移 001：ip_address/resource_type/resource_id/status/error_message）不一致。
-- 已部署过该中间版本的环境，表结构会缺列，导致审计写入持续失败。
-- 本迁移按权威定义补齐缺失列，使任意历史状态都能收敛到一致结构。

-- 表不存在时按权威定义创建（全新库已由 001 建好，此处为 no-op）
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

-- 补齐权威定义中的列（对已是正确结构的库全部为 no-op）
ALTER TABLE audit_logs ADD COLUMN user_id       TEXT;
ALTER TABLE audit_logs ADD COLUMN username      TEXT;
ALTER TABLE audit_logs ADD COLUMN action        TEXT;
ALTER TABLE audit_logs ADD COLUMN resource_type TEXT;
ALTER TABLE audit_logs ADD COLUMN resource_id   TEXT;
ALTER TABLE audit_logs ADD COLUMN resource_name TEXT;
ALTER TABLE audit_logs ADD COLUMN details       TEXT;
ALTER TABLE audit_logs ADD COLUMN ip_address    TEXT;
ALTER TABLE audit_logs ADD COLUMN status        TEXT;
ALTER TABLE audit_logs ADD COLUMN error_message TEXT;
ALTER TABLE audit_logs ADD COLUMN created_at    TEXT DEFAULT CURRENT_TIMESTAMP;

-- 中间版本遗留列若带 NOT NULL 约束，会阻塞按权威定义的插入，放开为可空
ALTER TABLE audit_logs ALTER COLUMN action DROP NOT NULL;



CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_user    ON audit_logs(user_id);

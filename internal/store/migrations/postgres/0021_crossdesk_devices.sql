-- CrossDesk 原生信令设备表（替代外部 crossdesk-server 的设备身份库）。
CREATE TABLE IF NOT EXISTS crossdesk_devices (
    device_id     TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL DEFAULT '',
    kind          TEXT NOT NULL DEFAULT 'desktop',
    online        INTEGER NOT NULL DEFAULT 0,
    last_seen     TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT ''
);

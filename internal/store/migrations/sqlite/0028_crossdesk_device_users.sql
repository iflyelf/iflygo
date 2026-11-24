-- CrossDesk 设备授权用户：普通 iFlyGo 用户可免密连接指定设备。
CREATE TABLE IF NOT EXISTS crossdesk_device_users (
    device_id  TEXT NOT NULL,
    username   TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, username)
);

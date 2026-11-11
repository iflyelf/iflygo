-- 网关健康状态：记录每网络当前使用的 DNS 网关，健康变化时递增客户端配置版本以自动切换。
CREATE TABLE IF NOT EXISTS gateway_health_state (
    network_id INTEGER PRIMARY KEY,
    gateway4   TEXT NOT NULL DEFAULT '',
    gateway6   TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);

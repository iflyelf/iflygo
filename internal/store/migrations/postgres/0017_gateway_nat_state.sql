-- 网关 NAT 自动应用状态：按网络 + 模式记录脚本指纹与 Agent 任务，避免重复执行。
CREATE TABLE IF NOT EXISTS gateway_nat_state (
    network_id      BIGINT NOT NULL,
    mode            TEXT NOT NULL,
    fingerprint     TEXT NOT NULL DEFAULT '',
    server_node_id  BIGINT NOT NULL DEFAULT 0,
    gateway         TEXT NOT NULL DEFAULT '',
    host_ip         TEXT NOT NULL DEFAULT '',
    task_id         BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT '',
    last_error      TEXT NOT NULL DEFAULT '',
    last_attempt_at TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (network_id, mode)
);

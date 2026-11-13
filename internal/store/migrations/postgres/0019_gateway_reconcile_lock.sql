-- 网关自动对账租约：多副本部署时避免并发重复下发 NAT 任务。
CREATE TABLE IF NOT EXISTS gateway_reconcile_lock (
    id           INTEGER PRIMARY KEY,
    locked_until TEXT NOT NULL DEFAULT ''
);

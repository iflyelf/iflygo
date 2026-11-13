-- NAT 任务失败重试计数与告警时间，避免长期失败循环。
ALTER TABLE gateway_nat_state ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE gateway_nat_state ADD COLUMN alerted_at TEXT NOT NULL DEFAULT '';

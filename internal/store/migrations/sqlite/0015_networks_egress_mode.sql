-- 网络出口模式：off（默认）/ dns（仅公网 DNS）/ full（全量公网出口）。
ALTER TABLE networks ADD COLUMN egress_mode TEXT NOT NULL DEFAULT '';

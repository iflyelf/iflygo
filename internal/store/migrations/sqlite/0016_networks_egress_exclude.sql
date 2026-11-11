-- 全量出口排除网段（逗号分隔 CIDR）；留空使用内置内网/控制面排除策略。
ALTER TABLE networks ADD COLUMN egress_exclude TEXT NOT NULL DEFAULT '';

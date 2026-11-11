-- 显式 DNS 网关（overlay IP）：多网关场景下指定公网 DNS 主机路由的下一跳。
-- 空字符串表示自动选择（默认路由 > gateway 节点 > 网络网关 > 任意服务端节点）。
ALTER TABLE networks ADD COLUMN dns_gateway TEXT NOT NULL DEFAULT '';

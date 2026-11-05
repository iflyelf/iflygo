-- 隧道连通性检查结果
--
-- 保存隧道地址后由服务端异步拨测一次（source=platform），也支持让 Agent
-- 从主机侧自检回连（source=agent，结果随心跳上报）。
-- 列表据此标红，避免运维必须手动点「测试」才能发现地址填错。
--
-- tunnel_check_requested_at 非空表示已请求主机侧自检，服务端在心跳响应中
-- 下发待测地址；Agent 返回结果后清空。超过有效期未响应则视为超时。

ALTER TABLE hosts ADD COLUMN tunnel_check_ok INTEGER;
ALTER TABLE hosts ADD COLUMN tunnel_check_source TEXT NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN tunnel_check_at TEXT;
ALTER TABLE hosts ADD COLUMN tunnel_check_latency_ms INT NOT NULL DEFAULT 0;
ALTER TABLE hosts ADD COLUMN tunnel_check_detail TEXT NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN tunnel_check_requested_at TEXT;

CREATE INDEX IF NOT EXISTS idx_hosts_tunnel_check
    ON hosts(tunnel_check_ok)
    WHERE tunnel_check_ok IS FALSE;

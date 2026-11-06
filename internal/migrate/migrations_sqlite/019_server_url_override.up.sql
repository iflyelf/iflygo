-- 将「平台侧隧道地址」重命名为「连接地址覆盖」（SQLite 版）
--
-- SQLite 3.25+ 支持 RENAME COLUMN；重命名后与 PG 路径的 server_url_override 对齐。
ALTER TABLE hosts RENAME COLUMN server_tunnel_url TO server_url_override;

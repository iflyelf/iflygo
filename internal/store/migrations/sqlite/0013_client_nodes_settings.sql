-- 每台主机的客户端参数覆盖（JSON）：use_server_dns/dns/mtu/interval/data_plane_mode。
-- 空字符串表示跟随网络级/全局默认。
ALTER TABLE client_nodes ADD COLUMN client_settings TEXT NOT NULL DEFAULT '';

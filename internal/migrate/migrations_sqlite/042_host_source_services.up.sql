-- 记录每台主机最近一次同步时运行的服务列表（SQLite 存 JSON 文本）。
ALTER TABLE hosts ADD COLUMN source_services TEXT NOT NULL DEFAULT '[]';
CREATE INDEX IF NOT EXISTS idx_hosts_source_services ON hosts(source_services);

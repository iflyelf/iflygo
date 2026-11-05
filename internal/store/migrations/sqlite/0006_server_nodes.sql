CREATE TABLE IF NOT EXISTS server_nodes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id  INTEGER NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    name        TEXT NOT NULL UNIQUE,
    roles       TEXT NOT NULL DEFAULT '',
    region      TEXT NOT NULL DEFAULT '',
    overlay_ip4 TEXT NOT NULL DEFAULT '',
    overlay_ip6 TEXT NOT NULL DEFAULT '',
    public_host TEXT NOT NULL DEFAULT '',
    public_port INTEGER NOT NULL DEFAULT 0,
    public_key  TEXT NOT NULL DEFAULT '',
    cert_pem    TEXT NOT NULL DEFAULT '',
    key_pem     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT ''
);

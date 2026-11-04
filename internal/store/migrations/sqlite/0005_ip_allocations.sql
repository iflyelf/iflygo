CREATE TABLE IF NOT EXISTS ip_allocations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    ip         TEXT NOT NULL,
    family     INTEGER NOT NULL DEFAULT 4,
    node_id    INTEGER,
    state      TEXT NOT NULL DEFAULT 'allocated',
    reserved   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT '',
    UNIQUE (network_id, ip)
);

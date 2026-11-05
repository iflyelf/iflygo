CREATE TABLE IF NOT EXISTS edge_services (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    host       TEXT NOT NULL UNIQUE,
    target     TEXT NOT NULL,
    protocol   TEXT NOT NULL DEFAULT 'http',
    tls_mode   TEXT NOT NULL DEFAULT 'terminate',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT ''
);

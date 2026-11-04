CREATE TABLE IF NOT EXISTS networks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    cidr4      TEXT NOT NULL DEFAULT '',
    cidr6      TEXT NOT NULL DEFAULT '',
    gateway4   TEXT NOT NULL DEFAULT '',
    gateway6   TEXT NOT NULL DEFAULT '',
    dns        TEXT NOT NULL DEFAULT '',
    mtu        INTEGER NOT NULL DEFAULT 1300,
    created_at TEXT NOT NULL DEFAULT ''
);

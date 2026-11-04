CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    is_admin      INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL DEFAULT ''
);

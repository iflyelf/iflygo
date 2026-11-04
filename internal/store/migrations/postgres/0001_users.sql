CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    is_admin      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TEXT NOT NULL DEFAULT ''
);

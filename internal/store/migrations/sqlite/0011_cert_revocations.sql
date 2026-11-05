CREATE TABLE IF NOT EXISTS cert_revocations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id  INTEGER NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    reason      TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS cas (
    id          BIGSERIAL PRIMARY KEY,
    network_id  BIGINT NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    cert_pem    TEXT NOT NULL,
    key_pem     TEXT NOT NULL,
    fingerprint TEXT NOT NULL DEFAULT '',
    expires_at  TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT ''
);

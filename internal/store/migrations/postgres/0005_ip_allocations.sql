CREATE TABLE IF NOT EXISTS ip_allocations (
    id         BIGSERIAL PRIMARY KEY,
    network_id BIGINT NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    ip         TEXT NOT NULL,
    family     INTEGER NOT NULL DEFAULT 4,
    node_id    BIGINT,
    state      TEXT NOT NULL DEFAULT 'allocated',
    reserved   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TEXT NOT NULL DEFAULT '',
    UNIQUE (network_id, ip)
);

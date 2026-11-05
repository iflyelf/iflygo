CREATE TABLE IF NOT EXISTS subnet_routes (
    id              BIGSERIAL PRIMARY KEY,
    network_id      BIGINT NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    cidr            TEXT NOT NULL,
    mtu             INTEGER NOT NULL DEFAULT 0,
    metric          INTEGER NOT NULL DEFAULT 0,
    mode            TEXT NOT NULL DEFAULT 'active_active',
    all_down_policy TEXT NOT NULL DEFAULT 'fail_closed',
    enabled         INTEGER NOT NULL DEFAULT 1,
    description     TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT ''
);

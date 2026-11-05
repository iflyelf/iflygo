CREATE TABLE IF NOT EXISTS route_gateways (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    route_id      INTEGER NOT NULL REFERENCES subnet_routes(id) ON DELETE CASCADE,
    node_id       INTEGER,
    overlay_ip    TEXT NOT NULL,
    weight        INTEGER NOT NULL DEFAULT 1,
    priority      INTEGER NOT NULL DEFAULT 0,
    health_target TEXT NOT NULL DEFAULT '',
    health_port   INTEGER NOT NULL DEFAULT 0,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL DEFAULT ''
);

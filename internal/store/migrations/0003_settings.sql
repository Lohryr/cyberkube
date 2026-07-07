-- Small key/value store for singleton platform settings (e.g. the
-- procedural world seed) that must stay stable across replicas and restarts.
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

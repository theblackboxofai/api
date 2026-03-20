CREATE TABLE IF NOT EXISTS scan_runs (
    id BIGSERIAL PRIMARY KEY,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    ranges_file TEXT NOT NULL,
    masscan_port INTEGER NOT NULL,
    masscan_rate INTEGER NOT NULL,
    discovered_hosts INTEGER NOT NULL DEFAULT 0,
    saved_hosts INTEGER NOT NULL DEFAULT 0,
    notes TEXT
);

CREATE TABLE IF NOT EXISTS server_scans (
    id BIGSERIAL PRIMARY KEY,
    scan_run_id BIGINT NOT NULL REFERENCES scan_runs(id) ON DELETE CASCADE,
    scanned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    server_url TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    ollama_version TEXT,
    response_json JSONB NOT NULL,
    model_count INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_server_scans_host_scanned_at
    ON server_scans (host, scanned_at DESC);

CREATE TABLE IF NOT EXISTS server_models (
    id BIGSERIAL PRIMARY KEY,
    server_scan_id BIGINT NOT NULL REFERENCES server_scans(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    model TEXT,
    digest TEXT,
    size_bytes BIGINT,
    modified_at TIMESTAMPTZ,
    details JSONB,
    raw_json JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_server_models_server_scan_id
    ON server_models (server_scan_id);

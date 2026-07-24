CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS tests (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    target_url       TEXT NOT NULL,
    virtual_users    INTEGER NOT NULL,
    duration_seconds INTEGER NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    test_id       UUID NOT NULL REFERENCES tests(id),
    status        TEXT NOT NULL,
    started_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    error_message TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS run_metric_snapshots (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id                UUID NOT NULL REFERENCES runs(id),
    ts                    TIMESTAMPTZ NOT NULL,
    elapsed_seconds       INTEGER NOT NULL,
    throughput_rps        DOUBLE PRECISION NOT NULL,
    avg_response_time_ms  DOUBLE PRECISION NOT NULL,
    error_rate_pct        DOUBLE PRECISION NOT NULL,
    sample_count          INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_run_metric_snapshots_run_id ON run_metric_snapshots(run_id, ts);

CREATE TABLE IF NOT EXISTS batches (
    batch_id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS events (
    batch_id TEXT NOT NULL REFERENCES batches(batch_id),
    event_index INTEGER NOT NULL,
    event_id TEXT,
    name TEXT NOT NULL,
    timestamp_ms BIGINT NOT NULL,
    project_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    funnel_id TEXT,
    user_id TEXT,
    anonymous_id TEXT,
    session_id TEXT,
    properties JSONB,
    context JSONB,
    received_at_ms BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (batch_id, event_index)
);

CREATE INDEX IF NOT EXISTS events_project_name_time_idx
    ON events(project_id, name, timestamp_ms);

CREATE INDEX IF NOT EXISTS events_org_time_idx
    ON events(org_id, timestamp_ms);

CREATE INDEX IF NOT EXISTS events_funnel_time_idx
    ON events(funnel_id, timestamp_ms);

CREATE INDEX IF NOT EXISTS events_user_time_idx
    ON events(user_id, timestamp_ms);

CREATE INDEX IF NOT EXISTS events_batch_id_idx
    ON events(batch_id);
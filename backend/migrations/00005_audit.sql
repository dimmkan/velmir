-- +goose Up
CREATE TABLE IF NOT EXISTS audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_user_id BIGINT NOT NULL REFERENCES users(id),
    action TEXT NOT NULL CHECK (action IN ('create', 'update')),
    schema_name TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    record_key JSONB NOT NULL,
    before_data JSONB,
    after_data JSONB,
    CONSTRAINT audit_events_payload_check CHECK (
        (action = 'create' AND before_data IS NULL AND after_data IS NOT NULL)
        OR (action = 'update' AND before_data IS NOT NULL AND after_data IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS audit_events_history_lookup_idx
    ON audit_events (schema_name, resource_name, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS audit_events_record_key_idx
    ON audit_events USING GIN (record_key);

-- +goose Down
DROP TABLE IF EXISTS audit_events;

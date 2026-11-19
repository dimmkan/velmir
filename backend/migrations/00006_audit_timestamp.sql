-- +goose Up
ALTER TABLE audit_events
    ALTER COLUMN occurred_at SET DEFAULT clock_timestamp();

-- +goose Down
ALTER TABLE audit_events
    ALTER COLUMN occurred_at SET DEFAULT now();

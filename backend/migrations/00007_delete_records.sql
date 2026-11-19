-- +goose Up
ALTER TABLE role_permissions DROP CONSTRAINT IF EXISTS role_permissions_permission_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'role_permissions'::regclass
          AND conname = 'role_permissions_permission_check'
    ) THEN
        ALTER TABLE role_permissions
            ADD CONSTRAINT role_permissions_permission_check
            CHECK (permission IN ('read', 'create', 'update', 'delete'));
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_action_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'audit_events'::regclass
          AND conname = 'audit_events_action_check'
    ) THEN
        ALTER TABLE audit_events
            ADD CONSTRAINT audit_events_action_check
            CHECK (action IN ('create', 'update', 'delete'));
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_payload_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'audit_events'::regclass
          AND conname = 'audit_events_payload_check'
    ) THEN
        ALTER TABLE audit_events
            ADD CONSTRAINT audit_events_payload_check CHECK (
                (action = 'create' AND before_data IS NULL AND after_data IS NOT NULL)
                OR (action = 'update' AND before_data IS NOT NULL AND after_data IS NOT NULL)
                OR (action = 'delete' AND before_data IS NOT NULL AND after_data IS NULL)
            );
    END IF;
END $$;
-- +goose StatementEnd

INSERT INTO role_permissions (role_id, schema_name, resource_name, permission)
SELECT r.id, '*', '*', 'delete'
FROM roles AS r
WHERE r.name = 'admin' AND r.is_system
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission = 'delete';
DELETE FROM audit_events WHERE action = 'delete';

ALTER TABLE role_permissions DROP CONSTRAINT IF EXISTS role_permissions_permission_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'role_permissions'::regclass
          AND conname = 'role_permissions_permission_check'
    ) THEN
        ALTER TABLE role_permissions
            ADD CONSTRAINT role_permissions_permission_check
            CHECK (permission IN ('read', 'create', 'update'));
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_action_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'audit_events'::regclass
          AND conname = 'audit_events_action_check'
    ) THEN
        ALTER TABLE audit_events
            ADD CONSTRAINT audit_events_action_check
            CHECK (action IN ('create', 'update'));
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_payload_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'audit_events'::regclass
          AND conname = 'audit_events_payload_check'
    ) THEN
        ALTER TABLE audit_events
            ADD CONSTRAINT audit_events_payload_check CHECK (
                (action = 'create' AND before_data IS NULL AND after_data IS NOT NULL)
                OR (action = 'update' AND before_data IS NOT NULL AND after_data IS NOT NULL)
            );
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Up
ALTER TABLE role_field_permissions
    DROP CONSTRAINT IF EXISTS role_field_permissions_action_check;

ALTER TABLE role_field_permissions
    ADD CONSTRAINT role_field_permissions_action_check
    CHECK (action IN ('read', 'create', 'update'));

INSERT INTO role_field_permissions (role_id, schema_name, resource_name, action, mode, columns)
SELECT rp.role_id, rp.schema_name, rp.resource_name, rp.permission, 'unrestricted', '[]'::jsonb
FROM role_permissions AS rp
WHERE rp.permission = 'read'
  AND rp.schema_name <> '*'
  AND rp.resource_name <> '*'
ON CONFLICT (role_id, schema_name, resource_name, action) DO NOTHING;

-- +goose Down
DELETE FROM role_field_permissions WHERE action = 'read';

ALTER TABLE role_field_permissions
    DROP CONSTRAINT IF EXISTS role_field_permissions_action_check;

ALTER TABLE role_field_permissions
    ADD CONSTRAINT role_field_permissions_action_check
    CHECK (action IN ('create', 'update'));

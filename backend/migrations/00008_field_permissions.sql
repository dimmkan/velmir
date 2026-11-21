-- +goose Up
CREATE TABLE IF NOT EXISTS role_field_permissions (
    role_id BIGINT NOT NULL,
    schema_name TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('create', 'update')),
    mode TEXT NOT NULL CHECK (mode IN ('unrestricted', 'allowlist')),
    columns JSONB NOT NULL DEFAULT '[]'::jsonb,
    PRIMARY KEY (role_id, schema_name, resource_name, action),
    FOREIGN KEY (role_id, schema_name, resource_name, action)
        REFERENCES role_permissions (role_id, schema_name, resource_name, permission)
        ON DELETE CASCADE,
    CHECK (jsonb_typeof(columns) = 'array'),
    CHECK (jsonb_array_length(columns) <= 1000),
    CHECK (mode = 'allowlist' OR columns = '[]'::jsonb)
);

CREATE INDEX IF NOT EXISTS role_field_permissions_lookup_idx
    ON role_field_permissions (schema_name, resource_name, action);

INSERT INTO role_field_permissions (role_id, schema_name, resource_name, action, mode, columns)
SELECT rp.role_id, rp.schema_name, rp.resource_name, rp.permission, 'unrestricted', '[]'::jsonb
FROM role_permissions AS rp
WHERE rp.permission IN ('create', 'update')
  AND rp.schema_name <> '*'
  AND rp.resource_name <> '*'
ON CONFLICT (role_id, schema_name, resource_name, action) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS role_field_permissions;

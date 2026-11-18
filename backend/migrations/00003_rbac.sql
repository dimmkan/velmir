-- +goose Up
CREATE TABLE IF NOT EXISTS roles (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    schema_name TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission IN ('read', 'create', 'update')),
    PRIMARY KEY (role_id, schema_name, resource_name, permission)
);

CREATE INDEX IF NOT EXISTS user_roles_role_id_idx ON user_roles (role_id);
CREATE INDEX IF NOT EXISTS role_permissions_lookup_idx ON role_permissions (schema_name, resource_name, permission);

INSERT INTO roles (name, description)
VALUES
    ('admin', 'Full access to supported record operations'),
    ('viewer', 'Read access to all resources')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, schema_name, resource_name, permission)
SELECT r.id, '*', '*', p.permission
FROM roles AS r
CROSS JOIN (VALUES ('read'), ('create'), ('update')) AS p(permission)
WHERE r.name = 'admin'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, schema_name, resource_name, permission)
SELECT r.id, '*', '*', 'read'
FROM roles AS r
WHERE r.name = 'viewer'
ON CONFLICT DO NOTHING;

INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM users AS u
CROSS JOIN roles AS r
WHERE r.name = 'admin'
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS roles;

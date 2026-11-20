package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"velmir/backend/internal/authorization"
)

var (
	ErrLastActiveAdmin   = errors.New("the last active administrator cannot be disabled or lose the admin role")
	ErrSelfDisable       = errors.New("you cannot disable your own account")
	ErrSelfAdminRemoval  = errors.New("you cannot remove your own admin role")
	ErrSystemRoleMutable = errors.New("system roles cannot be renamed or have their permissions changed")
	ErrRoleAlreadyExists = errors.New("role already exists")
)

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,99}$`)

type AdminValidationError struct {
	Field   string
	Message string
}

func (e *AdminValidationError) Error() string { return e.Message }

type Role struct {
	ID          int64
	Name        string
	Description string
	IsSystem    bool
	Permissions authorization.Permissions
}

type Administration struct {
	db TxBeginner
}

func NewAdministration(db TxBeginner) *Administration {
	return &Administration{db: db}
}

func (s *Administration) ListUsers(ctx context.Context) ([]UserSummary, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin user list: %w", err)
	}
	defer tx.Rollback(ctx)
	users, err := ListUsers(ctx, tx)
	if err != nil {
		return nil, err
	}
	return users, tx.Commit(ctx)
}

func (s *Administration) CreateUser(ctx context.Context, email, displayName, password string) (UserSummary, error) {
	if _, err := validateNewUserLogin(email); err != nil {
		return UserSummary{}, &AdminValidationError{Field: "email", Message: err.Error()}
	}
	if err := validateNewUserPassword(password); err != nil {
		return UserSummary{}, &AdminValidationError{Field: "password", Message: err.Error()}
	}
	if len(strings.TrimSpace(displayName)) > 200 {
		return UserSummary{}, &AdminValidationError{Field: "display_name", Message: "Display name must not exceed 200 characters"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UserSummary{}, fmt.Errorf("begin user creation: %w", err)
	}
	defer tx.Rollback(ctx)
	user, err := CreateUser(ctx, tx, email, displayName, password)
	if err != nil {
		return UserSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserSummary{}, fmt.Errorf("commit user creation: %w", err)
	}
	return UserSummary{ID: user.ID, Email: user.Email, DisplayName: user.DisplayName, IsActive: user.IsActive, Roles: []string{}}, nil
}

func (s *Administration) UpdateDisplayName(ctx context.Context, userID int64, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 200 {
		return &AdminValidationError{Field: "display_name", Message: "Display name is required and must not exceed 200 characters"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin display name update: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "UPDATE users SET display_name = $2, updated_at = $3 WHERE id = $1", userID, displayName, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("update display name: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return tx.Commit(ctx)
}

func (s *Administration) SetUserActive(ctx context.Context, actorID, userID int64, active bool) error {
	if !active && actorID == userID {
		return ErrSelfDisable
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin user status update: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "LOCK TABLE users IN EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("lock users for status update: %w", err)
	}
	var currentlyActive bool
	if err := tx.QueryRow(ctx, "SELECT is_active FROM users WHERE id = $1", userID).Scan(&currentlyActive); errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	} else if err != nil {
		return fmt.Errorf("find user for status update: %w", err)
	}
	if !active && currentlyActive {
		last, err := isLastActiveAdmin(ctx, tx, userID)
		if err != nil {
			return err
		}
		if last {
			return ErrLastActiveAdmin
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE users SET is_active = $2, updated_at = $3 WHERE id = $1", userID, active, time.Now().UTC()); err != nil {
		return fmt.Errorf("update user status: %w", err)
	}
	if !active {
		if err := revokeUserSessions(ctx, tx, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Administration) ResetPassword(ctx context.Context, userID int64, password string) error {
	if err := validateNewUserPassword(password); err != nil {
		return &AdminValidationError{Field: "password", Message: err.Error()}
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin password reset: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "UPDATE users SET password_hash = $2, updated_at = $3 WHERE id = $1", userID, passwordHash, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	if err := revokeUserSessions(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Administration) AssignRole(ctx context.Context, userID, roleID int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin role assignment: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := requireUser(ctx, tx, userID); err != nil {
		return err
	}
	if err := requireRole(ctx, tx, roleID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", userID, roleID); err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Administration) RemoveRole(ctx context.Context, actorID, userID, roleID int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin role removal: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "LOCK TABLE users IN EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("lock users for role removal: %w", err)
	}
	if err := requireUser(ctx, tx, userID); err != nil {
		return err
	}
	var roleName string
	if err := tx.QueryRow(ctx, "SELECT name FROM roles WHERE id = $1", roleID).Scan(&roleName); errors.Is(err, pgx.ErrNoRows) {
		return ErrRoleNotFound
	} else if err != nil {
		return fmt.Errorf("find role for removal: %w", err)
	}
	if actorID == userID && roleName == "admin" {
		return ErrSelfAdminRemoval
	}
	var assigned bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id = $1 AND role_id = $2)", userID, roleID).Scan(&assigned); err != nil {
		return fmt.Errorf("check role assignment: %w", err)
	}
	if assigned && roleName == "admin" {
		last, err := isLastActiveAdmin(ctx, tx, userID)
		if err != nil {
			return err
		}
		if last {
			return ErrLastActiveAdmin
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2", userID, roleID); err != nil {
		return fmt.Errorf("remove role: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Administration) ListRoles(ctx context.Context) ([]Role, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin role list: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.name, r.description, r.is_system,
		       rp.schema_name, rp.resource_name, rp.permission
		FROM roles AS r
		LEFT JOIN role_permissions AS rp ON rp.role_id = r.id
		ORDER BY r.name, rp.schema_name, rp.resource_name, rp.permission`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	roles := make([]Role, 0)
	indices := make(map[int64]int)
	grants := make(map[int64][]authorization.Grant)
	for rows.Next() {
		var role Role
		var grant authorization.Grant
		var schemaName, resourceName *string
		var operation *authorization.Operation
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem, &schemaName, &resourceName, &operation); err != nil {
			return nil, fmt.Errorf("read role: %w", err)
		}
		index, ok := indices[role.ID]
		if !ok {
			index = len(roles)
			indices[role.ID] = index
			roles = append(roles, role)
		}
		if schemaName != nil {
			grant = authorization.Grant{Schema: *schemaName, Resource: *resourceName, Operation: *operation}
			grants[role.ID] = append(grants[role.ID], grant)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}
	for index := range roles {
		roles[index].Permissions = authorization.EffectivePermissions(grants[roles[index].ID])
	}
	return roles, tx.Commit(ctx)
}

func (s *Administration) CreateRole(ctx context.Context, name, description string, permissions authorization.Permissions) (Role, error) {
	name, description, err := validateRole(name, description, permissions)
	if err != nil {
		return Role{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Role{}, fmt.Errorf("begin role creation: %w", err)
	}
	defer tx.Rollback(ctx)
	role := Role{Name: name, Description: description, Permissions: permissions}
	if err := tx.QueryRow(ctx, "INSERT INTO roles (name, description) VALUES ($1, $2) RETURNING id", name, description).Scan(&role.ID); err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return Role{}, ErrRoleAlreadyExists
		}
		return Role{}, fmt.Errorf("create role: %w", err)
	}
	if err := replacePermissions(ctx, tx, role.ID, permissions); err != nil {
		return Role{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Role{}, fmt.Errorf("commit role creation: %w", err)
	}
	return role, nil
}

func (s *Administration) UpdateRole(ctx context.Context, roleID int64, name, description string) error {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if !roleNamePattern.MatchString(name) {
		return &AdminValidationError{Field: "name", Message: "Role name must start with a lowercase letter and contain only lowercase letters, digits, underscores or hyphens (maximum 100 characters)"}
	}
	if len(description) > 500 {
		return &AdminValidationError{Field: "description", Message: "Role description must not exceed 500 characters"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin role update: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := requireCustomRole(ctx, tx, roleID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE roles SET name = $2, description = $3, updated_at = $4 WHERE id = $1", roleID, name, description, time.Now().UTC()); err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return ErrRoleAlreadyExists
		}
		return fmt.Errorf("update role: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Administration) ReplaceRolePermissions(ctx context.Context, roleID int64, permissions authorization.Permissions) error {
	if err := authorization.ValidatePermissions(permissions); err != nil {
		return &AdminValidationError{Field: "permissions", Message: err.Error()}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin permission update: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := requireCustomRole(ctx, tx, roleID); err != nil {
		return err
	}
	if err := replacePermissions(ctx, tx, roleID, permissions); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func validateRole(name, description string, permissions authorization.Permissions) (string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if !roleNamePattern.MatchString(name) {
		return "", "", &AdminValidationError{Field: "name", Message: "Role name must start with a lowercase letter and contain only lowercase letters, digits, underscores or hyphens (maximum 100 characters)"}
	}
	if len(description) > 500 {
		return "", "", &AdminValidationError{Field: "description", Message: "Role description must not exceed 500 characters"}
	}
	if err := authorization.ValidatePermissions(permissions); err != nil {
		return "", "", &AdminValidationError{Field: "permissions", Message: err.Error()}
	}
	return name, description, nil
}

func replacePermissions(ctx context.Context, tx pgx.Tx, roleID int64, permissions authorization.Permissions) error {
	if _, err := tx.Exec(ctx, "DELETE FROM role_permissions WHERE role_id = $1", roleID); err != nil {
		return fmt.Errorf("clear role permissions: %w", err)
	}
	for _, permission := range permissions {
		for _, operation := range permission.Operations {
			if _, err := tx.Exec(ctx, "INSERT INTO role_permissions (role_id, schema_name, resource_name, permission) VALUES ($1, $2, $3, $4)", roleID, permission.Schema, permission.Resource, operation); err != nil {
				return fmt.Errorf("save role permission: %w", err)
			}
		}
	}
	return nil
}

func requireUser(ctx context.Context, tx pgx.Tx, userID int64) error {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)", userID).Scan(&exists); err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	if !exists {
		return ErrUserNotFound
	}
	return nil
}

func requireRole(ctx context.Context, tx pgx.Tx, roleID int64) error {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM roles WHERE id = $1)", roleID).Scan(&exists); err != nil {
		return fmt.Errorf("find role: %w", err)
	}
	if !exists {
		return ErrRoleNotFound
	}
	return nil
}

func requireCustomRole(ctx context.Context, tx pgx.Tx, roleID int64) error {
	var system bool
	if err := tx.QueryRow(ctx, "SELECT is_system FROM roles WHERE id = $1 FOR UPDATE", roleID).Scan(&system); errors.Is(err, pgx.ErrNoRows) {
		return ErrRoleNotFound
	} else if err != nil {
		return fmt.Errorf("find role: %w", err)
	}
	if system {
		return ErrSystemRoleMutable
	}
	return nil
}

func isLastActiveAdmin(ctx context.Context, tx pgx.Tx, userID int64) (bool, error) {
	var targetIsActiveAdmin bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users AS u
			JOIN user_roles AS ur ON ur.user_id = u.id
			JOIN roles AS r ON r.id = ur.role_id
			WHERE u.id = $1 AND u.is_active AND r.name = 'admin' AND r.is_system
		)`, userID).Scan(&targetIsActiveAdmin); err != nil {
		return false, fmt.Errorf("check target administrator: %w", err)
	}
	if !targetIsActiveAdmin {
		return false, nil
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(DISTINCT u.id)
		FROM users AS u
		JOIN user_roles AS ur ON ur.user_id = u.id
		JOIN roles AS r ON r.id = ur.role_id
		WHERE u.is_active AND r.name = 'admin' AND r.is_system`).Scan(&count); err != nil {
		return false, fmt.Errorf("count active administrators: %w", err)
	}
	return count <= 1, nil
}

func revokeUserSessions(ctx context.Context, tx pgx.Tx, userID int64) error {
	if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}

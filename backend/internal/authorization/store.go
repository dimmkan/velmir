package authorization

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type DBTX interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Provider interface {
	Permissions(context.Context, int64) (Permissions, error)
	IsAdmin(context.Context, int64) (bool, error)
}

type Store struct {
	db DBTX
}

func NewStore(db DBTX) *Store {
	return &Store{db: db}
}

func (s *Store) Permissions(ctx context.Context, userID int64) (Permissions, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id, rp.schema_name, rp.resource_name, rp.permission
		FROM user_roles AS ur
		JOIN roles AS r ON r.id = ur.role_id
		JOIN role_permissions AS rp ON rp.role_id = r.id
		WHERE ur.user_id = $1
		ORDER BY rp.schema_name, rp.resource_name, rp.permission`, userID)
	if err != nil {
		return nil, fmt.Errorf("load effective permissions: %w", err)
	}
	grants := make([]Grant, 0)
	for rows.Next() {
		var grant Grant
		if err := rows.Scan(&grant.RoleID, &grant.Schema, &grant.Resource, &grant.Operation); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read effective permission: %w", err)
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate effective permissions: %w", err)
	}
	rows.Close()

	fieldRows, err := s.db.Query(ctx, `
		SELECT fp.role_id, fp.schema_name, fp.resource_name, fp.action, fp.mode, fp.columns
		FROM user_roles AS ur
		JOIN role_field_permissions AS fp ON fp.role_id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY fp.schema_name, fp.resource_name, fp.action`, userID)
	if err != nil {
		return nil, fmt.Errorf("load effective field permissions: %w", err)
	}
	defer fieldRows.Close()
	fieldGrants := make([]FieldGrant, 0)
	for fieldRows.Next() {
		var grant FieldGrant
		if err := fieldRows.Scan(&grant.RoleID, &grant.Schema, &grant.Resource, &grant.Action, &grant.Mode, &grant.Columns); err != nil {
			return nil, fmt.Errorf("read effective field permission: %w", err)
		}
		fieldGrants = append(fieldGrants, grant)
	}
	if err := fieldRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate effective field permissions: %w", err)
	}
	return EffectivePermissionsWithFields(grants, fieldGrants), nil
}

func (s *Store) IsAdmin(ctx context.Context, userID int64) (bool, error) {
	var isAdmin bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM user_roles AS ur
			JOIN roles AS r ON r.id = ur.role_id
			WHERE ur.user_id = $1 AND r.name = 'admin' AND r.is_system
		)`, userID).Scan(&isAdmin); err != nil {
		return false, fmt.Errorf("check administrator role: %w", err)
	}
	return isAdmin, nil
}

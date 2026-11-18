package authorization

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type DBTX interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type Provider interface {
	Permissions(context.Context, int64) (Permissions, error)
}

type Store struct {
	db DBTX
}

func NewStore(db DBTX) *Store {
	return &Store{db: db}
}

func (s *Store) Permissions(ctx context.Context, userID int64) (Permissions, error) {
	rows, err := s.db.Query(ctx, `
		SELECT rp.schema_name, rp.resource_name, rp.permission
		FROM user_roles AS ur
		JOIN roles AS r ON r.id = ur.role_id
		JOIN role_permissions AS rp ON rp.role_id = r.id
		WHERE ur.user_id = $1
		ORDER BY rp.schema_name, rp.resource_name, rp.permission`, userID)
	if err != nil {
		return nil, fmt.Errorf("load effective permissions: %w", err)
	}
	defer rows.Close()

	grants := make([]Grant, 0)
	for rows.Next() {
		var grant Grant
		if err := rows.Scan(&grant.Schema, &grant.Resource, &grant.Operation); err != nil {
			return nil, fmt.Errorf("read effective permission: %w", err)
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate effective permissions: %w", err)
	}
	return EffectivePermissions(grants), nil
}

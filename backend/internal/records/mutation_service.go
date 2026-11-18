package records

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"velmir/backend/internal/introspection"
)

type Writer struct {
	database databaseQuerier
}

func NewWriter(database databaseQuerier) *Writer {
	return &Writer{database: database}
}

func (w *Writer) Create(ctx context.Context, schemaName string, resource introspection.Resource, values RecordValues) (map[string]any, error) {
	query, args, err := BuildInsertQuery(schemaName, resource, values)
	if err != nil {
		return nil, err
	}
	return w.execute(ctx, query, args, resource, false)
}

func (w *Writer) Update(ctx context.Context, schemaName string, resource introspection.Resource, key string, values RecordValues) (map[string]any, error) {
	query, args, err := BuildUpdateQuery(schemaName, resource, key, values)
	if err != nil {
		return nil, err
	}
	return w.execute(ctx, query, args, resource, true)
}

func (w *Writer) execute(ctx context.Context, query string, args []any, resource introspection.Resource, allowMissing bool) (map[string]any, error) {
	rows, err := w.database.Query(ctx, query, args...)
	if err != nil {
		return nil, classifyMutationError(err, resource)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, classifyMutationError(err, resource)
		}
		if allowMissing {
			return nil, mutationError("RECORD_NOT_FOUND", "Record was not found")
		}
		return nil, errors.New("mutation returned no record")
	}
	record, err := scanRecord(rows)
	if err != nil {
		return nil, fmt.Errorf("read returned record: %w", err)
	}
	if rows.Next() {
		return nil, errors.New("mutation returned more than one record")
	}
	if err := rows.Err(); err != nil {
		return nil, classifyMutationError(err, resource)
	}
	return record, nil
}

func classifyMutationError(err error, resource introspection.Resource) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("execute record mutation: %w", err)
	}

	var result *MutationError
	switch postgresError.Code {
	case "23502":
		result = &MutationError{Code: "NOT_NULL_VIOLATION", Message: "A required field is missing"}
	case "23505":
		result = &MutationError{Code: "UNIQUE_VIOLATION", Message: "A record with these values already exists"}
	case "23503":
		result = &MutationError{Code: "FOREIGN_KEY_VIOLATION", Message: "A referenced record does not exist or the reference is invalid"}
	case "23514":
		result = &MutationError{Code: "CHECK_VIOLATION", Message: "A value violates a database check constraint"}
	default:
		return fmt.Errorf("execute record mutation: %w", err)
	}

	if postgresError.ColumnName != "" {
		if _, ok := findColumn(resource, postgresError.ColumnName); ok {
			result.FieldErrors = map[string]string{postgresError.ColumnName: result.Message}
		}
	}
	if postgresError.Code == "23503" {
		for _, foreignKey := range resource.ForeignKeys {
			if foreignKey.Name != postgresError.ConstraintName {
				continue
			}
			result.FieldErrors = make(map[string]string, len(foreignKey.Columns))
			for _, column := range foreignKey.Columns {
				result.FieldErrors[column] = result.Message
			}
		}
	}
	return result
}

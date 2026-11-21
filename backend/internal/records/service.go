package records

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"velmir/backend/internal/introspection"
)

type databaseQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type Service struct {
	database databaseQuerier
}

type ListResult struct {
	Records []map[string]any `json:"records"`
	Page    int64            `json:"page"`
	Limit   int              `json:"limit"`
	HasNext bool             `json:"has_next"`
}

func New(database databaseQuerier) *Service {
	return &Service{database: database}
}

func (s *Service) List(ctx context.Context, schemaName string, resource introspection.Resource, options ListOptions) (ListResult, error) {
	query, args := BuildSelectQuery(resource, schemaName, options)
	rows, err := s.database.Query(ctx, query, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("query records: %w", err)
	}
	defer rows.Close()

	result := ListResult{
		Records: make([]map[string]any, 0, options.Limit),
		Page:    options.Page,
		Limit:   options.Limit,
	}
	fields := rows.FieldDescriptions()
	for rows.Next() {
		values := make([]any, len(fields))
		scanTargets := make([]any, len(fields))
		for index, field := range fields {
			if field.DataTypeOID == pgtype.JSONOID || field.DataTypeOID == pgtype.JSONBOID {
				scanTargets[index] = new([]byte)
			} else {
				scanTargets[index] = &values[index]
			}
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return ListResult{}, fmt.Errorf("read record: %w", err)
		}
		if len(result.Records) == options.Limit {
			result.HasNext = true
			continue
		}

		record := make(map[string]any, len(values))
		for index, field := range fields {
			if _, ok := findColumn(resource, field.Name); !ok {
				continue
			}
			value := values[index]
			if rawJSON, ok := scanTargets[index].(*[]byte); ok {
				if *rawJSON == nil {
					value = nil
				} else {
					value = *rawJSON
				}
			}
			jsonValue, err := jsonValue(value, field.DataTypeOID)
			if err != nil {
				return ListResult{}, fmt.Errorf("convert column %q: %w", field.Name, err)
			}
			record[field.Name] = jsonValue
		}
		result.Records = append(result.Records, record)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("read records: %w", err)
	}
	return result, nil
}

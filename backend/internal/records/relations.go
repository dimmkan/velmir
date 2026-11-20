package records

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"velmir/backend/internal/introspection"
)

const (
	MaxRelationResolveRelations = 20
	MaxRelationResolveKeys      = 200
	MaxRelationResolveTotalKeys = 1000
)

var ErrRelationResolveForbidden = errors.New("referenced resource read permission is required")

type RelationResolveRequest struct {
	Relations []RelationResolveRelation `json:"relations"`
}

type RelationResolveRelation struct {
	Name string                       `json:"name"`
	Keys []map[string]json.RawMessage `json:"keys"`
}

type RelationResolvePlan struct {
	Name       string
	Schema     string
	Resource   introspection.Resource
	ForeignKey introspection.ForeignKey
	Keys       []RelationResolveKey
}

type RelationResolveKey struct {
	Values []any
}

type RelationResolveResult struct {
	Relations []ResolvedRelation `json:"relations"`
}

type ResolvedRelation struct {
	Name    string           `json:"name"`
	Records []map[string]any `json:"records"`
}

func PrepareRelationResolve(
	request RelationResolveRequest,
	current introspection.Resource,
	catalog introspection.Catalog,
	canRead func(string, string) bool,
) ([]RelationResolvePlan, error) {
	if len(request.Relations) > MaxRelationResolveRelations {
		return nil, validationError("INVALID_RELATION_RESOLVE", fmt.Sprintf("No more than %d relations are allowed", MaxRelationResolveRelations))
	}
	plans := make([]RelationResolvePlan, 0, len(request.Relations))
	seenRelations := make(map[string]bool)
	totalKeys := 0
	for _, input := range request.Relations {
		if seenRelations[input.Name] {
			return nil, validationError("INVALID_RELATION_RESOLVE", "Relation names must be unique")
		}
		seenRelations[input.Name] = true
		if len(input.Keys) > MaxRelationResolveKeys {
			return nil, validationError("INVALID_RELATION_RESOLVE", fmt.Sprintf("No more than %d keys per relation are allowed", MaxRelationResolveKeys))
		}
		totalKeys += len(input.Keys)
		if totalKeys > MaxRelationResolveTotalKeys {
			return nil, validationError("INVALID_RELATION_RESOLVE", fmt.Sprintf("No more than %d total keys are allowed", MaxRelationResolveTotalKeys))
		}

		foreignKey, ok := findForeignKey(current, input.Name)
		if !ok || len(foreignKey.Columns) == 0 || len(foreignKey.Columns) != len(foreignKey.ReferencedColumns) {
			return nil, validationError("INVALID_RELATION_RESOLVE", "Unknown or invalid relation")
		}
		if !canRead(foreignKey.ReferencedSchema, foreignKey.ReferencedTable) {
			return nil, ErrRelationResolveForbidden
		}
		referenced, ok := catalog.FindResource(foreignKey.ReferencedSchema, foreignKey.ReferencedTable)
		if !ok {
			return nil, validationError("INVALID_RELATION_RESOLVE", "Unknown or invalid relation")
		}
		plan := RelationResolvePlan{
			Name:       input.Name,
			Schema:     foreignKey.ReferencedSchema,
			Resource:   referenced,
			ForeignKey: foreignKey,
			Keys:       make([]RelationResolveKey, 0, len(input.Keys)),
		}
		seenKeys := make(map[string]bool)
		for _, key := range input.Keys {
			if len(key) != len(foreignKey.ReferencedColumns) {
				return nil, validationError("INVALID_RELATION_RESOLVE", "Each key must contain exactly the referenced columns")
			}
			values := make([]any, len(foreignKey.ReferencedColumns))
			nullKey := false
			for index, columnName := range foreignKey.ReferencedColumns {
				raw, present := key[columnName]
				if !present {
					return nil, validationError("INVALID_RELATION_RESOLVE", "Each key must contain exactly the referenced columns")
				}
				column, found := findColumn(referenced, columnName)
				if !found {
					return nil, validationError("INVALID_RELATION_RESOLVE", "Unknown or invalid relation")
				}
				if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					nullKey = true
					continue
				}
				value, err := parseRelationKeyValue(raw, column.DataType)
				if err != nil {
					return nil, validationError("INVALID_RELATION_RESOLVE", "A relation key value has an invalid type or format")
				}
				values[index] = value
			}
			if nullKey {
				continue
			}
			identity, err := json.Marshal(values)
			if err != nil {
				return nil, validationError("INVALID_RELATION_RESOLVE", "A relation key cannot be represented")
			}
			if seenKeys[string(identity)] {
				continue
			}
			seenKeys[string(identity)] = true
			plan.Keys = append(plan.Keys, RelationResolveKey{Values: values})
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func findForeignKey(resource introspection.Resource, name string) (introspection.ForeignKey, bool) {
	for _, foreignKey := range resource.ForeignKeys {
		if foreignKey.Name == name {
			return foreignKey, true
		}
	}
	return introspection.ForeignKey{}, false
}

func parseRelationKeyValue(raw json.RawMessage, dataType string) (any, error) {
	var text string
	quoted := len(raw) > 0 && raw[0] == '"'
	if quoted {
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
	} else {
		text = string(raw)
	}
	if baseType(dataType) == "boolean" && !quoted {
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	}
	if isTextType(dataType) && !quoted {
		return nil, errors.New("text key must be a JSON string")
	}
	if baseType(dataType) == "bytea" {
		if !quoted {
			return nil, errors.New("bytea key must be a Base64 string")
		}
		return base64.StdEncoding.DecodeString(text)
	}
	value, err := parseFilterValue(dataType, text)
	if err != nil {
		return nil, err
	}
	if baseType(dataType) == "uuid" {
		if id, ok := value.(string); ok {
			return strings.ToLower(id), nil
		}
	}
	return value, nil
}

func BuildRelationResolveQuery(plan RelationResolvePlan) (string, []any) {
	columns := relationResultColumns(plan.Resource, plan.ForeignKey.ReferencedColumns)
	selected := make([]string, 0, len(columns))
	for _, column := range columns {
		selected = append(selected, quoteIdentifier(column))
	}
	var query strings.Builder
	query.WriteString("SELECT ")
	query.WriteString(strings.Join(selected, ", "))
	query.WriteString(" FROM ")
	query.WriteString(quoteIdentifier(plan.Schema))
	query.WriteByte('.')
	query.WriteString(quoteIdentifier(plan.Resource.Name))
	query.WriteString(" WHERE (")

	args := make([]any, 0, len(plan.Keys)*len(plan.ForeignKey.ReferencedColumns))
	tuples := make([]string, 0, len(plan.Keys))
	for _, key := range plan.Keys {
		placeholders := make([]string, 0, len(key.Values))
		for index, value := range key.Values {
			args = append(args, value)
			placeholder := fmt.Sprintf("$%d", len(args))
			switch baseType(filterType(plan.Resource, plan.ForeignKey.ReferencedColumns[index])) {
			case "numeric", "decimal":
				placeholder += "::numeric"
			case "uuid":
				placeholder += "::uuid"
			}
			placeholders = append(placeholders, placeholder)
		}
		tuples = append(tuples, "("+strings.Join(placeholders, ", ")+")")
	}
	quotedColumns := make([]string, 0, len(plan.ForeignKey.ReferencedColumns))
	for _, column := range plan.ForeignKey.ReferencedColumns {
		quotedColumns = append(quotedColumns, quoteIdentifier(column))
	}
	query.WriteString("(" + strings.Join(quotedColumns, ", ") + ") IN (")
	query.WriteString(strings.Join(tuples, ", "))
	query.WriteString("))")
	return query.String(), args
}

func relationResultColumns(resource introspection.Resource, referencedColumns []string) []string {
	columns := make([]string, 0, len(referencedColumns)+len(resource.PrimaryKey)+len(resource.Columns))
	seen := make(map[string]bool)
	add := func(column string) {
		if !seen[column] {
			seen[column] = true
			columns = append(columns, column)
		}
	}
	for _, column := range referencedColumns {
		add(column)
	}
	for _, column := range resource.PrimaryKey {
		add(column)
	}
	for _, column := range resource.Columns {
		if isTextType(column.DataType) {
			add(column.Name)
		}
	}
	return columns
}

func (s *Service) ResolveRelations(ctx context.Context, plans []RelationResolvePlan) (RelationResolveResult, error) {
	result := RelationResolveResult{Relations: make([]ResolvedRelation, 0, len(plans))}
	for _, plan := range plans {
		resolved := ResolvedRelation{Name: plan.Name, Records: make([]map[string]any, 0, len(plan.Keys))}
		if len(plan.Keys) > 0 {
			query, args := BuildRelationResolveQuery(plan)
			rows, err := s.database.Query(ctx, query, args...)
			if err != nil {
				return RelationResolveResult{}, fmt.Errorf("query related records: %w", err)
			}
			for rows.Next() {
				record, err := scanRecord(rows)
				if err != nil {
					rows.Close()
					return RelationResolveResult{}, fmt.Errorf("read related record: %w", err)
				}
				resolved.Records = append(resolved.Records, record)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return RelationResolveResult{}, fmt.Errorf("read related records: %w", err)
			}
			rows.Close()
		}
		result.Relations = append(result.Relations, resolved)
	}
	return result, nil
}

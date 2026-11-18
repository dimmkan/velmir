package records

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"velmir/backend/internal/introspection"
)

type RecordValues map[string]json.RawMessage

type MutationError struct {
	Code        string
	Message     string
	FieldErrors map[string]string
}

func (e *MutationError) Error() string {
	return e.Message
}

func BuildInsertQuery(schemaName string, resource introspection.Resource, values RecordValues) (string, []any, error) {
	if resource.Kind != "table" {
		return "", nil, mutationError("RESOURCE_READ_ONLY", "Records can only be created in tables")
	}
	if err := validateKnownFields(resource, values); err != nil {
		return "", nil, err
	}

	columns := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	placeholders := make([]string, 0, len(values))
	for _, column := range resource.Columns {
		raw, present := values[column.Name]
		if !present {
			if !column.Nullable && column.Default == nil && column.Identity == "" && !column.Generated {
				return "", nil, fieldValidationError(column.Name, "This field is required")
			}
			continue
		}
		if !column.Editable {
			return "", nil, fieldValidationError(column.Name, "This field cannot be edited")
		}
		value, err := parseMutationValue(column, raw)
		if err != nil {
			return "", nil, err
		}
		columns = append(columns, quoteIdentifier(column.Name))
		args = append(args, value)
		placeholders = append(placeholders, mutationPlaceholder(column, len(args)))
	}

	var query strings.Builder
	query.WriteString("INSERT INTO ")
	query.WriteString(quoteIdentifier(schemaName))
	query.WriteByte('.')
	query.WriteString(quoteIdentifier(resource.Name))
	if len(columns) == 0 {
		query.WriteString(" DEFAULT VALUES")
	} else {
		query.WriteString(" (")
		query.WriteString(strings.Join(columns, ", "))
		query.WriteString(") VALUES (")
		query.WriteString(strings.Join(placeholders, ", "))
		query.WriteByte(')')
	}
	query.WriteString(" RETURNING ")
	query.WriteString(returningColumns(resource))
	return query.String(), args, nil
}

func BuildUpdateQuery(schemaName string, resource introspection.Resource, rawKey string, values RecordValues) (string, []any, error) {
	if resource.Kind != "table" {
		return "", nil, mutationError("RESOURCE_READ_ONLY", "Records can only be updated in tables")
	}
	if len(resource.PrimaryKey) == 0 {
		return "", nil, mutationError("RESOURCE_READ_ONLY", "Records without a primary key cannot be updated")
	}
	if err := validateKnownFields(resource, values); err != nil {
		return "", nil, err
	}
	if len(values) == 0 {
		return "", nil, mutationError("INVALID_RECORD", "At least one field must be provided")
	}

	assignments := make([]string, 0, len(values))
	args := make([]any, 0, len(values)+len(resource.PrimaryKey))
	for _, column := range resource.Columns {
		raw, present := values[column.Name]
		if !present {
			continue
		}
		if column.PrimaryKey {
			return "", nil, fieldValidationError(column.Name, "Primary key fields cannot be updated")
		}
		if !column.Editable {
			return "", nil, fieldValidationError(column.Name, "This field cannot be edited")
		}
		value, err := parseMutationValue(column, raw)
		if err != nil {
			return "", nil, err
		}
		args = append(args, value)
		assignments = append(assignments, quoteIdentifier(column.Name)+" = "+mutationPlaceholder(column, len(args)))
	}

	keyValues, err := parseRecordKey(resource, rawKey)
	if err != nil {
		return "", nil, err
	}
	conditions := make([]string, 0, len(resource.PrimaryKey))
	for _, keyColumn := range resource.PrimaryKey {
		column, _ := findColumn(resource, keyColumn)
		value, err := parseMutationValue(column, keyValues[keyColumn])
		if err != nil {
			return "", nil, err
		}
		args = append(args, value)
		conditions = append(conditions, quoteIdentifier(keyColumn)+" = "+mutationPlaceholder(column, len(args)))
	}

	var query strings.Builder
	query.WriteString("UPDATE ")
	query.WriteString(quoteIdentifier(schemaName))
	query.WriteByte('.')
	query.WriteString(quoteIdentifier(resource.Name))
	query.WriteString(" SET ")
	query.WriteString(strings.Join(assignments, ", "))
	query.WriteString(" WHERE ")
	query.WriteString(strings.Join(conditions, " AND "))
	query.WriteString(" RETURNING ")
	query.WriteString(returningColumns(resource))
	return query.String(), args, nil
}

func parseRecordKey(resource introspection.Resource, rawKey string) (RecordValues, error) {
	var key RecordValues
	if err := json.Unmarshal([]byte(rawKey), &key); err != nil || len(key) != len(resource.PrimaryKey) {
		return nil, mutationError("INVALID_RECORD_KEY", "Record key must be a JSON object containing exactly the primary key fields")
	}
	for _, name := range resource.PrimaryKey {
		if _, ok := key[name]; !ok {
			return nil, mutationError("INVALID_RECORD_KEY", "Record key must contain exactly the primary key fields")
		}
	}
	for name := range key {
		if !contains(resource.PrimaryKey, name) {
			return nil, mutationError("INVALID_RECORD_KEY", "Record key contains an unknown primary key field")
		}
	}
	return key, nil
}

func validateKnownFields(resource introspection.Resource, values RecordValues) error {
	for name := range values {
		if _, ok := findColumn(resource, name); !ok {
			return mutationError("INVALID_RECORD", "Record contains a field that is not present in schema metadata")
		}
	}
	return nil
}

func parseMutationValue(column introspection.Column, raw json.RawMessage) (any, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if !column.Nullable {
			return nil, fieldValidationError(column.Name, "This field cannot be null")
		}
		return nil, nil
	}
	if len(column.EnumValues) > 0 {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil || !contains(column.EnumValues, value) {
			return nil, fieldValidationError(column.Name, "Expected one of the allowed enum values")
		}
		return value, nil
	}

	switch baseType(column.DataType) {
	case "text", "character varying", "character", "citext", "name":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldValidationError(column.Name, "Expected a string")
		}
		if !column.Nullable && strings.TrimSpace(value) == "" {
			return nil, fieldValidationError(column.Name, "This field is required")
		}
		return value, nil
	case "boolean":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldValidationError(column.Name, "Expected true or false")
		}
		return value, nil
	case "smallint", "integer", "bigint", "real", "double precision", "numeric", "decimal":
		value, err := numericInput(raw)
		if err != nil {
			return nil, fieldValidationError(column.Name, "Expected a number or numeric string")
		}
		parsed, err := parseFilterValue(column.DataType, value)
		if err != nil {
			return nil, fieldValidationError(column.Name, numericValidationMessage(column.DataType))
		}
		return parsed, nil
	case "date", "timestamp without time zone", "timestamp with time zone", "uuid":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldValidationError(column.Name, "Expected a string")
		}
		parsed, err := parseFilterValue(column.DataType, value)
		if err != nil {
			return nil, fieldValidationError(column.Name, err.Error())
		}
		return parsed, nil
	case "json", "jsonb":
		if !json.Valid(raw) {
			return nil, fieldValidationError(column.Name, "Expected valid JSON")
		}
		return json.RawMessage(append([]byte(nil), raw...)), nil
	default:
		return nil, fieldValidationError(column.Name, "This PostgreSQL type is not editable")
	}
}

func numericInput(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return "", err
	}
	return number.String(), nil
}

func numericValidationMessage(dataType string) string {
	switch baseType(dataType) {
	case "smallint", "integer", "bigint":
		return "Expected an integer in the PostgreSQL type range"
	case "real", "double precision":
		return "Expected a finite number"
	default:
		return "Expected a decimal number in the PostgreSQL numeric range"
	}
}

func mutationPlaceholder(column introspection.Column, position int) string {
	placeholder := fmt.Sprintf("$%d", position)
	switch baseType(column.DataType) {
	case "numeric", "decimal", "uuid", "json", "jsonb":
		placeholder += "::" + baseType(column.DataType)
	}
	return placeholder
}

func returningColumns(resource introspection.Resource) string {
	columns := make([]string, 0, len(resource.Columns))
	for _, column := range resource.Columns {
		columns = append(columns, quoteIdentifier(column.Name))
	}
	if len(columns) == 0 {
		return "*"
	}
	return strings.Join(columns, ", ")
}

func fieldValidationError(field, message string) error {
	return &MutationError{
		Code:        "INVALID_RECORD",
		Message:     "Record validation failed",
		FieldErrors: map[string]string{field: message},
	}
}

func mutationError(code, message string) error {
	return &MutationError{Code: code, Message: message}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

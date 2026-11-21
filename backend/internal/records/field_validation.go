package records

import (
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
)

func ValidateMutationFieldMetadata(resource introspection.Resource, action authorization.Operation, values RecordValues) error {
	for name := range values {
		column, ok := findColumn(resource, name)
		if !ok {
			return mutationError("INVALID_RECORD", "Record contains a field that is not present in schema metadata")
		}
		if action == authorization.Update && column.PrimaryKey {
			return fieldValidationError(column.Name, "Primary key fields cannot be updated")
		}
		if !column.Editable {
			return fieldValidationError(column.Name, "This field cannot be edited")
		}
	}
	return nil
}

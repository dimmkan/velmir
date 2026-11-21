package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

const (
	createDisabledRequired = "required_field_unavailable"
	updateDisabledNoFields = "no_writable_fields"
)

func applyFieldPermissions(schemaName string, resource *introspection.Resource, permissions authorization.Permissions, writeEnabled bool) {
	createAccess := permissions.EffectiveFields(schemaName, resource.Name, authorization.Create)
	updateAccess := permissions.EffectiveFields(schemaName, resource.Name, authorization.Update)
	readAccess := permissions.EffectiveFields(schemaName, resource.Name, authorization.Read)
	resource.CreatableFields = effectiveResourceFields(*resource, createAccess, authorization.Create)
	resource.UpdatableFields = effectiveResourceFields(*resource, updateAccess, authorization.Update)
	resource.ReadableFields = effectiveReadableFields(*resource, readAccess)
	resource.CanCreate = writeEnabled && resource.Kind == "table" && createAccess.Granted && !hasUnavailableRequiredField(*resource, resource.CreatableFields)
	if createAccess.Granted && resource.Kind == "table" && hasUnavailableRequiredField(*resource, resource.CreatableFields) {
		resource.CreateDisabledReason = createDisabledRequired
	}
	resource.CanUpdate = writeEnabled && resource.Kind == "table" && len(resource.PrimaryKey) > 0 && updateAccess.Granted && len(resource.UpdatableFields) > 0
	if updateAccess.Granted && resource.Kind == "table" && len(resource.PrimaryKey) > 0 && len(resource.UpdatableFields) == 0 {
		resource.UpdateDisabledReason = updateDisabledNoFields
	}
	readable := fieldSet(resource.ReadableFields)
	visible := fieldSet(resource.ReadableFields)
	creatableFields := fieldSet(resource.CreatableFields)
	updatableFields := fieldSet(resource.UpdatableFields)
	resource.WriteOnlyFields = make([]string, 0)
	for _, column := range resource.Columns {
		_, creatable := creatableFields[column.Name]
		_, updatable := updatableFields[column.Name]
		if _, ok := readable[column.Name]; !ok && (creatable || updatable) {
			resource.WriteOnlyFields = append(resource.WriteOnlyFields, column.Name)
			visible[column.Name] = struct{}{}
		}
	}
	columns := make([]introspection.Column, 0, len(visible))
	for _, column := range resource.Columns {
		if _, ok := visible[column.Name]; ok {
			columns = append(columns, column)
		}
	}
	resource.Columns = columns
}

func effectiveResourceFields(resource introspection.Resource, access authorization.FieldAccess, action authorization.Operation) []string {
	fields := make([]string, 0)
	if !access.Granted {
		return fields
	}
	for _, column := range resource.Columns {
		if !column.Editable || action == authorization.Update && column.PrimaryKey {
			continue
		}
		if access.Allows(column.Name) {
			fields = append(fields, column.Name)
		}
	}
	return fields
}

func hasUnavailableRequiredField(resource introspection.Resource, fields []string) bool {
	allowed := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		allowed[field] = struct{}{}
	}
	for _, column := range resource.Columns {
		if column.Nullable || column.Default != nil || column.Identity != "" || column.Generated {
			continue
		}
		if _, ok := allowed[column.Name]; !ok {
			return true
		}
	}
	return false
}

func catalogHasRealWrite(catalog introspection.Catalog, permissions authorization.Permissions) bool {
	for _, schema := range catalog.Schemas {
		for _, resource := range schema.Resources {
			if resource.CanCreate || resource.CanUpdate {
				return true
			}
			if resource.Kind == "table" && len(resource.PrimaryKey) > 0 && permissions.Allows(schema.Name, resource.Name, authorization.Delete) {
				return true
			}
		}
	}
	return false
}

func enforceMutationFields(w http.ResponseWriter, r *http.Request, resource introspection.Resource, action authorization.Operation, values records.RecordValues) bool {
	if err := records.ValidateMutationFieldMetadata(resource, action, values); err != nil {
		writeMutationError(w, err)
		return false
	}
	schemaName := resourceSchemaName(r)
	access := currentPermissions(r).EffectiveFields(schemaName, resource.Name, action)
	if !access.Granted {
		writeForbidden(w)
		return false
	}
	fields := effectiveResourceFields(resource, access, action)
	allowed := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		allowed[field] = struct{}{}
	}
	fieldErrors := make(map[string]string)
	for field := range values {
		if _, ok := allowed[field]; !ok {
			fieldErrors[field] = "This field is not permitted for this action"
		}
	}
	if len(fieldErrors) > 0 {
		writeMutationError(w, &records.MutationError{
			Code: "FIELD_FORBIDDEN", Message: "One or more fields are not permitted for this action", FieldErrors: fieldErrors,
		})
		return false
	}
	if action == authorization.Create && hasUnavailableRequiredField(resource, fields) {
		writeMutationError(w, &records.MutationError{
			Code: "CREATE_IMPOSSIBLE", Message: "This role cannot create records because a required field is unavailable",
		})
		return false
	}
	if action == authorization.Update && len(fields) == 0 {
		writeMutationError(w, &records.MutationError{Code: "NO_WRITABLE_FIELDS", Message: "No fields are writable for this action"})
		return false
	}
	return true
}

func resourceSchemaName(r *http.Request) string {
	return chi.URLParam(r, "schema")
}

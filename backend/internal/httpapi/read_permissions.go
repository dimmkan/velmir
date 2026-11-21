package httpapi

import (
	"bytes"
	"encoding/json"

	"velmir/backend/internal/audit"
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
)

func effectiveReadableFields(resource introspection.Resource, access authorization.FieldAccess) []string {
	fields := make([]string, 0, len(resource.Columns))
	if !access.Granted {
		return fields
	}
	for _, column := range resource.Columns {
		if column.PrimaryKey || access.Allows(column.Name) {
			fields = append(fields, column.Name)
		}
	}
	return fields
}

func fieldSet(fields []string) map[string]struct{} {
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	return set
}

func resourceWithColumns(resource introspection.Resource, fields []string) introspection.Resource {
	allowed := fieldSet(fields)
	columns := make([]introspection.Column, 0, len(fields))
	for _, column := range resource.Columns {
		if _, ok := allowed[column.Name]; ok {
			columns = append(columns, column)
		}
	}
	resource.Columns = columns
	return resource
}

func filterRecordFields(record map[string]any, fields []string) map[string]any {
	if record == nil {
		return nil
	}
	allowed := fieldSet(fields)
	filtered := make(map[string]any, len(fields))
	for name, value := range record {
		if _, ok := allowed[name]; ok {
			filtered[name] = value
		}
	}
	return filtered
}

func filterHistoryFields(result audit.HistoryResult, fields []string) (audit.HistoryResult, error) {
	events := make([]audit.Event, len(result.Events))
	copy(events, result.Events)
	result.Events = events
	allowed := fieldSet(fields)
	for index := range result.Events {
		before, err := filterHistoryRecord(result.Events[index].Before, allowed)
		if err != nil {
			return audit.HistoryResult{}, err
		}
		after, err := filterHistoryRecord(result.Events[index].After, allowed)
		if err != nil {
			return audit.HistoryResult{}, err
		}
		result.Events[index].Before = before
		result.Events[index].After = after
	}
	return result, nil
}

func filterHistoryRecord(raw json.RawMessage, allowed map[string]struct{}) (json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return raw, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var record map[string]any
	if err := decoder.Decode(&record); err != nil {
		return nil, err
	}
	for name := range record {
		if _, ok := allowed[name]; !ok {
			delete(record, name)
		}
	}
	return json.Marshal(record)
}

func readableColumn(permissions authorization.Permissions, schemaName string, resource introspection.Resource, columnName string) bool {
	for _, primaryKey := range resource.PrimaryKey {
		if primaryKey == columnName {
			return true
		}
	}
	return permissions.EffectiveFields(schemaName, resource.Name, authorization.Read).Allows(columnName)
}

func filterRelationFieldMetadata(catalog *introspection.Catalog) {
	for schemaIndex := range catalog.Schemas {
		for resourceIndex := range catalog.Schemas[schemaIndex].Resources {
			resource := &catalog.Schemas[schemaIndex].Resources[resourceIndex]
			readable := fieldSet(resource.ReadableFields)
			foreignKeys := make([]introspection.ForeignKey, 0, len(resource.ForeignKeys))
			for _, foreignKey := range resource.ForeignKeys {
				localReadable := true
				for _, column := range foreignKey.Columns {
					if _, ok := readable[column]; !ok {
						localReadable = false
					}
				}
				referenced, ok := catalog.FindResource(foreignKey.ReferencedSchema, foreignKey.ReferencedTable)
				if !ok {
					continue
				}
				referencedReadable := fieldSet(referenced.ReadableFields)
				for _, column := range foreignKey.ReferencedColumns {
					if _, ok := referencedReadable[column]; !ok {
						localReadable = false
					}
				}
				if localReadable {
					foreignKeys = append(foreignKeys, foreignKey)
				}
			}
			resource.ForeignKeys = foreignKeys
			resource.ReverseRelations = make([]introspection.ReverseRelation, 0)
		}
	}
	for _, schema := range catalog.Schemas {
		for _, child := range schema.Resources {
			for _, foreignKey := range child.ForeignKeys {
				for parentSchemaIndex := range catalog.Schemas {
					parentSchema := &catalog.Schemas[parentSchemaIndex]
					if parentSchema.Name != foreignKey.ReferencedSchema {
						continue
					}
					for parentIndex := range parentSchema.Resources {
						parent := &parentSchema.Resources[parentIndex]
						if parent.Name == foreignKey.ReferencedTable {
							parent.ReverseRelations = append(parent.ReverseRelations, introspection.ReverseRelation{
								Name: foreignKey.Name, Schema: schema.Name, Resource: child.Name,
								Columns:           append([]string(nil), foreignKey.Columns...),
								ReferencedColumns: append([]string(nil), foreignKey.ReferencedColumns...),
							})
						}
					}
				}
			}
		}
	}
}

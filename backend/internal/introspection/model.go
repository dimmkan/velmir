package introspection

import (
	"sort"
	"strings"
)

type Catalog struct {
	Schemas      []Schema `json:"schemas"`
	WriteEnabled bool     `json:"write_enabled"`
}

type Schema struct {
	Name      string     `json:"name"`
	Resources []Resource `json:"resources"`
}

type Resource struct {
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Columns     []Column     `json:"columns"`
	PrimaryKey  []string     `json:"primary_key"`
	ForeignKeys []ForeignKey `json:"foreign_keys"`
}

type Column struct {
	Name       string   `json:"name"`
	DataType   string   `json:"data_type"`
	Nullable   bool     `json:"nullable"`
	Default    *string  `json:"default"`
	PrimaryKey bool     `json:"primary_key"`
	Identity   string   `json:"identity"`
	Generated  bool     `json:"generated"`
	Editable   bool     `json:"editable"`
	EnumValues []string `json:"enum_values"`
}

type ForeignKey struct {
	Name              string   `json:"name"`
	Columns           []string `json:"columns"`
	ReferencedSchema  string   `json:"referenced_schema"`
	ReferencedTable   string   `json:"referenced_table"`
	ReferencedColumns []string `json:"referenced_columns"`
}

func (c Catalog) FindResource(schemaName, resourceName string) (Resource, bool) {
	for _, schema := range c.Schemas {
		if schema.Name != schemaName {
			continue
		}
		for _, resource := range schema.Resources {
			if resource.Name == resourceName {
				return resource, true
			}
		}
	}
	return Resource{}, false
}

type objectRow struct {
	Schema string
	Name   string
	Kind   string
}

type columnRow struct {
	Schema     string
	Resource   string
	Name       string
	DataType   string
	Nullable   bool
	Default    *string
	Identity   string
	Generated  bool
	EnumValues []string
}

type primaryKeyRow struct {
	Schema   string
	Resource string
	Column   string
}

type foreignKeyRow struct {
	Schema           string
	Resource         string
	Name             string
	Column           string
	ReferencedSchema string
	ReferencedTable  string
	ReferencedColumn string
}

func buildCatalog(objects []objectRow, columns []columnRow, primaryKeys []primaryKeyRow, foreignKeys []foreignKeyRow) Catalog {
	catalog := Catalog{Schemas: make([]Schema, 0)}
	schemaIndexes := make(map[string]int)
	resourceIndexes := make(map[string]int)
	foreignKeyIndexes := make(map[string]int)

	for _, object := range objects {
		if isSystemSchema(object.Schema) {
			continue
		}

		schemaIndex, ok := schemaIndexes[object.Schema]
		if !ok {
			schemaIndex = len(catalog.Schemas)
			schemaIndexes[object.Schema] = schemaIndex
			catalog.Schemas = append(catalog.Schemas, Schema{
				Name:      object.Schema,
				Resources: make([]Resource, 0),
			})
		}

		resourceKey := object.Schema + "\x00" + object.Name
		resourceIndexes[resourceKey] = len(catalog.Schemas[schemaIndex].Resources)
		catalog.Schemas[schemaIndex].Resources = append(catalog.Schemas[schemaIndex].Resources, Resource{
			Name:        object.Name,
			Kind:        object.Kind,
			Columns:     make([]Column, 0),
			PrimaryKey:  make([]string, 0),
			ForeignKeys: make([]ForeignKey, 0),
		})
	}

	for _, column := range columns {
		if isSystemSchema(column.Schema) {
			continue
		}
		resourceKey := column.Schema + "\x00" + column.Resource
		resourceIndex, ok := resourceIndexes[resourceKey]
		if !ok {
			continue
		}
		schemaIndex := schemaIndexes[column.Schema]
		resource := &catalog.Schemas[schemaIndex].Resources[resourceIndex]
		enumValues := append([]string(nil), column.EnumValues...)
		if enumValues == nil {
			enumValues = make([]string, 0)
		}
		resource.Columns = append(resource.Columns, Column{
			Name:       column.Name,
			DataType:   column.DataType,
			Nullable:   column.Nullable,
			Default:    column.Default,
			Identity:   column.Identity,
			Generated:  column.Generated,
			Editable:   resource.Kind == "table" && !column.Generated && column.Identity != "always" && supportsMutationType(column.DataType, enumValues),
			EnumValues: enumValues,
		})
	}

	for _, primaryKey := range primaryKeys {
		if isSystemSchema(primaryKey.Schema) {
			continue
		}
		resourceKey := primaryKey.Schema + "\x00" + primaryKey.Resource
		resourceIndex, ok := resourceIndexes[resourceKey]
		if !ok {
			continue
		}
		schemaIndex := schemaIndexes[primaryKey.Schema]
		resource := &catalog.Schemas[schemaIndex].Resources[resourceIndex]
		resource.PrimaryKey = append(resource.PrimaryKey, primaryKey.Column)
		for index := range resource.Columns {
			if resource.Columns[index].Name == primaryKey.Column {
				resource.Columns[index].PrimaryKey = true
			}
		}
	}

	for _, foreignKey := range foreignKeys {
		if isSystemSchema(foreignKey.Schema) || isSystemSchema(foreignKey.ReferencedSchema) {
			continue
		}
		resourceKey := foreignKey.Schema + "\x00" + foreignKey.Resource
		resourceIndex, ok := resourceIndexes[resourceKey]
		if !ok {
			continue
		}
		schemaIndex := schemaIndexes[foreignKey.Schema]
		resource := &catalog.Schemas[schemaIndex].Resources[resourceIndex]
		foreignKeyKey := resourceKey + "\x00" + foreignKey.Name
		foreignKeyIndex, ok := foreignKeyIndexes[foreignKeyKey]
		if !ok {
			foreignKeyIndex = len(resource.ForeignKeys)
			foreignKeyIndexes[foreignKeyKey] = foreignKeyIndex
			resource.ForeignKeys = append(resource.ForeignKeys, ForeignKey{
				Name:              foreignKey.Name,
				Columns:           make([]string, 0),
				ReferencedSchema:  foreignKey.ReferencedSchema,
				ReferencedTable:   foreignKey.ReferencedTable,
				ReferencedColumns: make([]string, 0),
			})
		}
		resource.ForeignKeys[foreignKeyIndex].Columns = append(resource.ForeignKeys[foreignKeyIndex].Columns, foreignKey.Column)
		resource.ForeignKeys[foreignKeyIndex].ReferencedColumns = append(resource.ForeignKeys[foreignKeyIndex].ReferencedColumns, foreignKey.ReferencedColumn)
	}

	for schemaIndex := range catalog.Schemas {
		sort.Slice(catalog.Schemas[schemaIndex].Resources, func(i, j int) bool {
			return catalog.Schemas[schemaIndex].Resources[i].Name < catalog.Schemas[schemaIndex].Resources[j].Name
		})
	}
	sort.Slice(catalog.Schemas, func(i, j int) bool {
		return catalog.Schemas[i].Name < catalog.Schemas[j].Name
	})

	return catalog
}

func isSystemSchema(name string) bool {
	return name == "information_schema" || len(name) >= 3 && name[:3] == "pg_"
}

func supportsMutationType(dataType string, enumValues []string) bool {
	if len(enumValues) > 0 {
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(dataType))
	if index := strings.IndexByte(normalized, '('); index >= 0 {
		normalized = normalized[:index]
	}
	switch strings.TrimSpace(normalized) {
	case "text", "character varying", "character", "citext", "name",
		"boolean", "smallint", "integer", "bigint", "real", "double precision",
		"numeric", "decimal", "date", "timestamp without time zone",
		"timestamp with time zone", "uuid", "json", "jsonb":
		return true
	default:
		return false
	}
}

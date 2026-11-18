package introspection

import (
	"reflect"
	"testing"
)

func TestBuildCatalogTransformsAndGroupsMetadata(t *testing.T) {
	defaultID := "nextval('books_id_seq'::regclass)"
	objects := []objectRow{
		{Schema: "public", Name: "books", Kind: "table"},
		{Schema: "public", Name: "books_overview", Kind: "view"},
		{Schema: "archive", Name: "old_books", Kind: "table"},
		{Schema: "pg_catalog", Name: "pg_class", Kind: "view"},
	}
	columns := []columnRow{
		{Schema: "public", Resource: "books", Name: "id", DataType: "bigint", Nullable: false, Default: &defaultID, Identity: "always", EnumValues: []string{}},
		{Schema: "public", Resource: "books", Name: "author_id", DataType: "bigint", Nullable: false, EnumValues: []string{}},
		{Schema: "public", Resource: "books_overview", Name: "title", DataType: "character varying(255)", Nullable: true},
		{Schema: "archive", Resource: "old_books", Name: "id", DataType: "bigint", Nullable: false},
		{Schema: "pg_catalog", Resource: "pg_class", Name: "oid", DataType: "oid", Nullable: false},
	}
	primaryKeys := []primaryKeyRow{
		{Schema: "public", Resource: "books", Column: "id"},
		{Schema: "pg_catalog", Resource: "pg_class", Column: "oid"},
	}
	foreignKeys := []foreignKeyRow{
		{Schema: "public", Resource: "books", Name: "books_author_id_fkey", Column: "author_id", ReferencedSchema: "public", ReferencedTable: "authors", ReferencedColumn: "id"},
	}

	got := buildCatalog(objects, columns, primaryKeys, foreignKeys)

	if len(got.Schemas) != 2 {
		t.Fatalf("schemas = %d, want 2", len(got.Schemas))
	}
	if got.Schemas[0].Name != "archive" || got.Schemas[1].Name != "public" {
		t.Fatalf("schemas are not sorted: %+v", got.Schemas)
	}
	books := got.Schemas[1].Resources[0]
	if books.Name != "books" || books.Kind != "table" {
		t.Errorf("books resource = %+v", books)
	}
	if len(books.Columns) != 2 || books.Columns[0].Default == nil || *books.Columns[0].Default != defaultID {
		t.Errorf("books columns = %+v", books.Columns)
	}
	if books.Columns[0].Editable || books.Columns[0].Identity != "always" || !books.Columns[0].PrimaryKey || !books.Columns[1].Editable {
		t.Errorf("column editability metadata = %+v", books.Columns)
	}
	if books.Columns[0].EnumValues == nil {
		t.Error("enum_values must be an empty array")
	}
	if !reflect.DeepEqual(books.PrimaryKey, []string{"id"}) {
		t.Errorf("primary key = %v", books.PrimaryKey)
	}
	if len(books.ForeignKeys) != 1 {
		t.Fatalf("foreign keys = %d, want 1", len(books.ForeignKeys))
	}
	foreignKey := books.ForeignKeys[0]
	if foreignKey.ReferencedTable != "authors" || !reflect.DeepEqual(foreignKey.Columns, []string{"author_id"}) {
		t.Errorf("foreign key = %+v", foreignKey)
	}
	view := got.Schemas[1].Resources[1]
	if view.Kind != "view" || view.Columns[0].DataType != "character varying(255)" {
		t.Errorf("view = %+v", view)
	}
}

func TestBuildCatalogFiltersSystemSchemas(t *testing.T) {
	systemSchemas := []string{"information_schema", "pg_catalog", "pg_toast", "pg_temp_1"}
	objects := make([]objectRow, 0, len(systemSchemas)+1)
	for _, schema := range systemSchemas {
		objects = append(objects, objectRow{Schema: schema, Name: "hidden", Kind: "table"})
	}
	objects = append(objects, objectRow{Schema: "public", Name: "visible", Kind: "table"})

	catalog := buildCatalog(objects, nil, nil, nil)

	if len(catalog.Schemas) != 1 || catalog.Schemas[0].Name != "public" {
		t.Fatalf("catalog schemas = %+v, want only public", catalog.Schemas)
	}
	if len(catalog.Schemas[0].Resources) != 1 || catalog.Schemas[0].Resources[0].Name != "visible" {
		t.Fatalf("resources = %+v, want only visible", catalog.Schemas[0].Resources)
	}
}

func TestBuildCatalogReturnsEmptyArrays(t *testing.T) {
	catalog := buildCatalog(nil, nil, nil, nil)
	if catalog.Schemas == nil || len(catalog.Schemas) != 0 {
		t.Fatalf("schemas = %#v, want empty array", catalog.Schemas)
	}
}

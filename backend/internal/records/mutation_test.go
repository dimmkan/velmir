package records

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"velmir/backend/internal/introspection"
)

func mutationResource() introspection.Resource {
	defaultID := "nextval('items_id_seq'::regclass)"
	defaultCreated := "now()"
	return introspection.Resource{
		Name:       `odd"table`,
		Kind:       "table",
		PrimaryKey: []string{"tenant_id", "id"},
		Columns: []introspection.Column{
			{Name: "tenant_id", DataType: "bigint", PrimaryKey: true, Editable: true},
			{Name: "id", DataType: "bigint", Default: &defaultID, PrimaryKey: true, Identity: "always"},
			{Name: "title", DataType: "character varying(255)", Editable: true},
			{Name: "bio", DataType: "text", Nullable: true, Editable: true},
			{Name: "price", DataType: "numeric(20,4)", Nullable: true, Editable: true},
			{Name: "active", DataType: "boolean", Nullable: true, Editable: true},
			{Name: "published_on", DataType: "date", Nullable: true, Editable: true},
			{Name: "created_at", DataType: "timestamp with time zone", Default: &defaultCreated, Editable: true},
			{Name: "external_id", DataType: "uuid", Nullable: true, Editable: true},
			{Name: "payload", DataType: "jsonb", Nullable: true, Editable: true},
			{Name: "state", DataType: "item_state", Nullable: true, Editable: true, EnumValues: []string{"draft", "published"}},
			{Name: "unsupported", DataType: "bytea", Nullable: true},
		},
		ForeignKeys: []introspection.ForeignKey{{Name: "items_tenant_fk", Columns: []string{"tenant_id"}}},
	}
}

func rawValues(values map[string]string) RecordValues {
	result := make(RecordValues, len(values))
	for name, value := range values {
		result[name] = json.RawMessage(value)
	}
	return result
}

func TestBuildInsertQueryOmitsGeneratedAndDefaults(t *testing.T) {
	query, args, err := BuildInsertQuery(`odd"schema`, mutationResource(), rawValues(map[string]string{
		"tenant_id": `"9223372036854775807"`,
		"title":     `"Safe title"`,
		"price":     `"12345678901234567890.0001"`,
		"active":    `false`,
		"payload":   `{"big":9223372036854775807}`,
		"state":     `"draft"`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`INSERT INTO "odd""schema"."odd""table"`,
		`"tenant_id", "title", "price", "active", "payload", "state"`,
		`$3::numeric`,
		`$5::jsonb`,
		`RETURNING "tenant_id", "id", "title"`,
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("query does not contain %q:\n%s", fragment, query)
		}
	}
	insertClause := strings.Split(query, " RETURNING ")[0]
	if strings.Contains(insertClause, `"id"`) || strings.Contains(insertClause, `"created_at"`) {
		t.Errorf("generated/default fields were not omitted: %s", query)
	}
	if len(args) != 6 || args[0] != int64(9223372036854775807) || args[1] != "Safe title" || args[2] != "12345678901234567890.0001" || args[3] != false {
		t.Errorf("args = %#v", args)
	}
	if _, ok := args[4].(json.RawMessage); !ok {
		t.Errorf("JSON argument has type %T", args[4])
	}
}

func TestBuildInsertQueryCanUseOnlyDefaults(t *testing.T) {
	resource := mutationResource()
	resource.PrimaryKey = []string{"id"}
	resource.Columns = []introspection.Column{
		{Name: "id", DataType: "bigint", Default: mutationResource().Columns[1].Default, PrimaryKey: true, Identity: "always"},
		{Name: "note", DataType: "text", Nullable: true, Editable: true},
	}
	query, args, err := BuildInsertQuery("public", resource, RecordValues{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "DEFAULT VALUES") || len(args) != 0 {
		t.Errorf("query = %s, args = %#v", query, args)
	}
}

func TestBuildInsertQueryAllowsEmptyNullableText(t *testing.T) {
	_, args, err := BuildInsertQuery("public", mutationResource(), rawValues(map[string]string{
		"tenant_id": `"1"`,
		"title":     `"Required"`,
		"bio":       `""`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 3 || args[2] != "" {
		t.Errorf("args = %#v, want empty nullable text", args)
	}
}

func TestBuildUpdateQuerySupportsCompositePrimaryKey(t *testing.T) {
	query, args, err := BuildUpdateQuery("public", mutationResource(), `{"tenant_id":"7","id":"9"}`, rawValues(map[string]string{
		"title": `"Updated"`,
		"price": `null`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`SET "title" = $1, "price" = $2::numeric`,
		`WHERE "tenant_id" = $3 AND "id" = $4`,
		`RETURNING "tenant_id", "id", "title"`,
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("query does not contain %q:\n%s", fragment, query)
		}
	}
	want := []any{"Updated", nil, int64(7), int64(9)}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %#v, want %#v", args, want)
	}
}

func TestMutationValidation(t *testing.T) {
	tests := []struct {
		name   string
		values RecordValues
		field  string
		code   string
	}{
		{name: "required", values: rawValues(map[string]string{"tenant_id": `"1"`}), field: "title", code: "INVALID_RECORD"},
		{name: "unknown field", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `"x"`, `title"; DROP TABLE items;--`: `"x"`}), code: "INVALID_RECORD"},
		{name: "identity", values: rawValues(map[string]string{"tenant_id": `"1"`, "id": `"2"`, "title": `"x"`}), field: "id", code: "INVALID_RECORD"},
		{name: "unsupported", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `"x"`, "unsupported": `"AAH/"`}), field: "unsupported", code: "INVALID_RECORD"},
		{name: "null required", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `null`}), field: "title", code: "INVALID_RECORD"},
		{name: "empty required text", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `""`}), field: "title", code: "INVALID_RECORD"},
		{name: "whitespace required text", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `" \t\n "`}), field: "title", code: "INVALID_RECORD"},
		{name: "boolean", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `"x"`, "active": `"true"`}), field: "active", code: "INVALID_RECORD"},
		{name: "integer", values: rawValues(map[string]string{"tenant_id": `"1.5"`, "title": `"x"`}), field: "tenant_id", code: "INVALID_RECORD"},
		{name: "numeric", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `"x"`, "price": `"1e999999999"`}), field: "price", code: "INVALID_RECORD"},
		{name: "date", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `"x"`, "published_on": `"2025-02-30"`}), field: "published_on", code: "INVALID_RECORD"},
		{name: "timestamp", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `"x"`, "created_at": `"2025-01-01 10:00"`}), field: "created_at", code: "INVALID_RECORD"},
		{name: "uuid", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `"x"`, "external_id": `"not-a-uuid"`}), field: "external_id", code: "INVALID_RECORD"},
		{name: "JSON", values: RecordValues{"tenant_id": json.RawMessage(`"1"`), "title": json.RawMessage(`"x"`), "payload": json.RawMessage(`{invalid}`)}, field: "payload", code: "INVALID_RECORD"},
		{name: "enum", values: rawValues(map[string]string{"tenant_id": `"1"`, "title": `"x"`, "state": `"other"`}), field: "state", code: "INVALID_RECORD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := BuildInsertQuery("public", mutationResource(), tt.values)
			assertMutationError(t, err, tt.code, tt.field)
		})
	}
}

func TestUpdateRejectsPrimaryKeysAndReadOnlyResources(t *testing.T) {
	_, _, err := BuildUpdateQuery("public", mutationResource(), `{"tenant_id":"1","id":"2"}`, rawValues(map[string]string{"id": `"3"`}))
	assertMutationError(t, err, "INVALID_RECORD", "id")

	resource := mutationResource()
	resource.PrimaryKey = nil
	_, _, err = BuildUpdateQuery("public", resource, `{}`, rawValues(map[string]string{"title": `"x"`}))
	assertMutationError(t, err, "RESOURCE_READ_ONLY", "")

	resource = mutationResource()
	resource.Kind = "view"
	_, _, err = BuildInsertQuery("public", resource, RecordValues{})
	assertMutationError(t, err, "RESOURCE_READ_ONLY", "")
}

func TestRecordKeyValidation(t *testing.T) {
	values := rawValues(map[string]string{"title": `"x"`})
	for _, key := range []string{`not-json`, `{}`, `{"tenant_id":"1"}`, `{"tenant_id":"1","id":"2","extra":"3"}`, `{"tenant_id":null,"id":"2"}`} {
		_, _, err := BuildUpdateQuery("public", mutationResource(), key, values)
		if err == nil {
			t.Errorf("key %s was accepted", key)
		}
	}
}

func TestClassifyMutationErrors(t *testing.T) {
	tests := []struct {
		code  string
		want  string
		field string
	}{
		{code: "23502", want: "NOT_NULL_VIOLATION", field: "title"},
		{code: "23505", want: "UNIQUE_VIOLATION", field: "title"},
		{code: "23503", want: "FOREIGN_KEY_VIOLATION", field: "tenant_id"},
		{code: "23514", want: "CHECK_VIOLATION", field: "title"},
	}
	for _, tt := range tests {
		err := classifyMutationError(&pgconn.PgError{Code: tt.code, ColumnName: "title", ConstraintName: "items_tenant_fk", Message: "secret SQL"}, mutationResource())
		assertMutationError(t, err, tt.want, tt.field)
		if strings.Contains(err.Error(), "secret SQL") {
			t.Errorf("database details leaked: %v", err)
		}
	}
	if err := classifyMutationError(errors.New("secret SQL"), mutationResource()); err == nil {
		t.Fatal("generic error = nil")
	}
}

func assertMutationError(t *testing.T, err error, code, field string) {
	t.Helper()
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) {
		t.Fatalf("error = %T %v, want MutationError", err, err)
	}
	if mutationErr.Code != code {
		t.Errorf("code = %q, want %q", mutationErr.Code, code)
	}
	if field != "" && mutationErr.FieldErrors[field] == "" {
		t.Errorf("field errors = %#v, want %q", mutationErr.FieldErrors, field)
	}
}

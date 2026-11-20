package records

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"velmir/backend/internal/introspection"
)

func relationTestCatalog() (introspection.Catalog, introspection.Resource) {
	authors := introspection.Resource{
		Name: `odd"authors`, Kind: "table", PrimaryKey: []string{"id"},
		Columns: []introspection.Column{
			{Name: "id", DataType: "bigint"},
			{Name: "name", DataType: "text"},
			{Name: "secret_number", DataType: "numeric"},
		},
	}
	parents := introspection.Resource{
		Name: "parents", Kind: "table", PrimaryKey: []string{"tenant_id", "external_id"},
		Columns: []introspection.Column{
			{Name: "tenant_id", DataType: "bigint"},
			{Name: "external_id", DataType: "uuid"},
			{Name: "display_name", DataType: "text"},
			{Name: "settings", DataType: "jsonb"},
		},
	}
	books := introspection.Resource{
		Name: "books", Kind: "table",
		ForeignKeys: []introspection.ForeignKey{{
			Name: "books_author_fk", Columns: []string{"author_id"},
			ReferencedSchema: "public", ReferencedTable: `odd"authors`, ReferencedColumns: []string{"id"},
		}},
	}
	children := introspection.Resource{
		Name: "children", Kind: "table",
		ForeignKeys: []introspection.ForeignKey{{
			Name: "children_parent_fk", Columns: []string{"tenant_id", "parent_id"},
			ReferencedSchema: "public", ReferencedTable: "parents", ReferencedColumns: []string{"tenant_id", "external_id"},
		}},
	}
	catalog := introspection.Catalog{Schemas: []introspection.Schema{{Name: "public", Resources: []introspection.Resource{authors, parents, books, children}}}}
	return catalog, books
}

func rawKey(t *testing.T, value any) map[string]json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &key); err != nil {
		t.Fatal(err)
	}
	return key
}

func allowAllRelations(string, string) bool { return true }

func TestPrepareRelationResolveSimpleDeduplicatesAndSkipsNull(t *testing.T) {
	catalog, books := relationTestCatalog()
	plans, err := PrepareRelationResolve(RelationResolveRequest{Relations: []RelationResolveRelation{{
		Name: "books_author_fk",
		Keys: []map[string]json.RawMessage{
			rawKey(t, map[string]any{"id": "9223372036854775807"}),
			rawKey(t, map[string]any{"id": "9223372036854775807"}),
			rawKey(t, map[string]any{"id": nil}),
			{"id": json.RawMessage(" null ")},
		},
	}}}, books, catalog, allowAllRelations)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Keys) != 1 || !reflect.DeepEqual(plans[0].Keys[0].Values, []any{int64(9223372036854775807)}) {
		t.Fatalf("plans = %+v", plans)
	}
	query, args := BuildRelationResolveQuery(plans[0])
	for _, fragment := range []string{
		`SELECT "id", "name" FROM "public"."odd""authors"`,
		`WHERE (("id") IN (($1)))`,
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("query does not contain %q: %s", fragment, query)
		}
	}
	if strings.Contains(query, "secret_number") || strings.Contains(query, "9223372036854775807") {
		t.Errorf("query selects unrelated data or interpolates a key: %s", query)
	}
	if !reflect.DeepEqual(args, []any{int64(9223372036854775807)}) {
		t.Errorf("args = %#v", args)
	}
}

func TestPrepareRelationResolveCompositeAndExactValues(t *testing.T) {
	catalog, _ := relationTestCatalog()
	children, _ := catalog.FindResource("public", "children")
	uuid := "00112233-4455-6677-8899-AABBCCDDEEFF"
	plans, err := PrepareRelationResolve(RelationResolveRequest{Relations: []RelationResolveRelation{{
		Name: "children_parent_fk",
		Keys: []map[string]json.RawMessage{
			rawKey(t, map[string]any{"tenant_id": "9223372036854775807", "external_id": uuid}),
			rawKey(t, map[string]any{"tenant_id": nil, "external_id": uuid}),
		},
	}}}, children, catalog, allowAllRelations)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans[0].Keys) != 1 {
		t.Fatalf("composite keys = %+v", plans[0].Keys)
	}
	query, args := BuildRelationResolveQuery(plans[0])
	for _, fragment := range []string{
		`SELECT "tenant_id", "external_id", "display_name" FROM "public"."parents"`,
		`("tenant_id", "external_id") IN (($1, $2::uuid))`,
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("query does not contain %q: %s", fragment, query)
		}
	}
	if strings.Contains(query, "settings") || !reflect.DeepEqual(args, []any{int64(9223372036854775807), strings.ToLower(uuid)}) {
		t.Errorf("query = %s, args = %#v", query, args)
	}
}

func TestPrepareRelationResolveEmptyKeysDoNotBuildSQLRows(t *testing.T) {
	catalog, books := relationTestCatalog()
	plans, err := PrepareRelationResolve(RelationResolveRequest{Relations: []RelationResolveRelation{{Name: "books_author_fk", Keys: []map[string]json.RawMessage{}}}}, books, catalog, allowAllRelations)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Keys) != 0 {
		t.Fatalf("plans = %+v", plans)
	}
}

func TestPrepareRelationResolveRejectsUnknownAndMalformedInput(t *testing.T) {
	catalog, books := relationTestCatalog()
	for _, test := range []struct {
		name  string
		input RelationResolveRequest
	}{
		{name: "unknown relation", input: RelationResolveRequest{Relations: []RelationResolveRelation{{Name: "missing; DROP TABLE books", Keys: nil}}}},
		{name: "unknown key column", input: RelationResolveRequest{Relations: []RelationResolveRelation{{Name: "books_author_fk", Keys: []map[string]json.RawMessage{rawKey(t, map[string]any{"missing": "1"})}}}}},
		{name: "extra key column", input: RelationResolveRequest{Relations: []RelationResolveRelation{{Name: "books_author_fk", Keys: []map[string]json.RawMessage{rawKey(t, map[string]any{"id": "1", "name": "Alice"})}}}}},
		{name: "SQL value is not numeric", input: RelationResolveRequest{Relations: []RelationResolveRelation{{Name: "books_author_fk", Keys: []map[string]json.RawMessage{rawKey(t, map[string]any{"id": "1 OR true"})}}}}},
		{name: "duplicate relation", input: RelationResolveRequest{Relations: []RelationResolveRelation{{Name: "books_author_fk"}, {Name: "books_author_fk"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := PrepareRelationResolve(test.input, books, catalog, allowAllRelations); err == nil {
				t.Fatal("invalid request was accepted")
			}
		})
	}
}

func TestPrepareRelationResolveLimits(t *testing.T) {
	catalog, books := relationTestCatalog()
	tooManyRelations := RelationResolveRequest{Relations: make([]RelationResolveRelation, MaxRelationResolveRelations+1)}
	if _, err := PrepareRelationResolve(tooManyRelations, books, catalog, allowAllRelations); err == nil {
		t.Fatal("relation limit was not enforced")
	}
	keys := make([]map[string]json.RawMessage, MaxRelationResolveKeys+1)
	for index := range keys {
		keys[index] = rawKey(t, map[string]any{"id": index + 1})
	}
	tooManyKeys := RelationResolveRequest{Relations: []RelationResolveRelation{{Name: "books_author_fk", Keys: keys}}}
	if _, err := PrepareRelationResolve(tooManyKeys, books, catalog, allowAllRelations); err == nil {
		t.Fatal("key limit was not enforced")
	}

	current := introspection.Resource{ForeignKeys: make([]introspection.ForeignKey, 0, 6)}
	request := RelationResolveRequest{Relations: make([]RelationResolveRelation, 0, 6)}
	for index := 0; index < 6; index++ {
		name := strings.Repeat("r", index+1)
		current.ForeignKeys = append(current.ForeignKeys, introspection.ForeignKey{
			Name: name, Columns: []string{"author_id"}, ReferencedSchema: "public", ReferencedTable: `odd"authors`, ReferencedColumns: []string{"id"},
		})
		request.Relations = append(request.Relations, RelationResolveRelation{Name: name, Keys: keys[:MaxRelationResolveKeys]})
	}
	if _, err := PrepareRelationResolve(request, current, catalog, allowAllRelations); err == nil {
		t.Fatal("total key limit was not enforced")
	}
}

func TestPrepareRelationResolveRequiresReferencedRead(t *testing.T) {
	catalog, books := relationTestCatalog()
	_, err := PrepareRelationResolve(RelationResolveRequest{Relations: []RelationResolveRelation{{Name: "books_author_fk"}}}, books, catalog, func(string, string) bool { return false })
	if err != ErrRelationResolveForbidden {
		t.Fatalf("error = %v, want ErrRelationResolveForbidden", err)
	}
}

package records

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRelationResolveFiltersHiddenLabelsAndRequiresReadableKeys(t *testing.T) {
	catalog, books := relationTestCatalog()
	canReadField := func(_, _, column string) bool { return column != "name" }
	plans, err := PrepareRelationResolve(RelationResolveRequest{Relations: []RelationResolveRelation{{
		Name: "books_author_fk", Keys: []map[string]json.RawMessage{rawKey(t, map[string]any{"id": "1"})},
	}}}, books, "public", catalog, allowAllRelations, canReadField)
	if err != nil {
		t.Fatal(err)
	}
	query, _ := BuildRelationResolveQuery(plans[0])
	if strings.Contains(query, `"name"`) || !strings.Contains(query, `SELECT "id"`) {
		t.Fatalf("hidden label in relation query: %s", query)
	}

	_, err = PrepareRelationResolve(RelationResolveRequest{Relations: []RelationResolveRelation{{Name: "books_author_fk"}}}, books, "public", catalog, allowAllRelations,
		func(_, resource, column string) bool { return resource != "books" || column != "author_id" })
	if !errors.Is(err, ErrRelationResolveForbidden) {
		t.Fatalf("hidden local FK error = %v", err)
	}
}

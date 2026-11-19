package records

import (
	"strings"
	"testing"
)

func TestBuildSelectForUpdateAndCanonicalCompositeKey(t *testing.T) {
	query, args, err := BuildSelectForUpdateQuery(`odd"schema`, mutationResource(), `{"id":"9","tenant_id":"7"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`SELECT "tenant_id", "id", "title"`, `FROM "odd""schema"."odd""table"`, `WHERE "tenant_id" = $1 AND "id" = $2`, "FOR UPDATE"} {
		if !strings.Contains(query, fragment) {
			t.Errorf("query does not contain %q: %s", fragment, query)
		}
	}
	if len(args) != 2 || args[0] != int64(7) || args[1] != int64(9) {
		t.Errorf("args = %#v", args)
	}
	key, err := CanonicalRecordKey(mutationResource(), `{"id":"9","tenant_id":"7"}`)
	if err != nil {
		t.Fatal(err)
	}
	if string(key) != `{"id":"9","tenant_id":"7"}` {
		t.Errorf("canonical key = %s", key)
	}
	for _, invalid := range []string{`{"id":"not-a-number","tenant_id":"7"}`, `{"id":"9"}`, `{"id":null,"tenant_id":"7"}`} {
		if _, err := CanonicalRecordKey(mutationResource(), invalid); err == nil {
			t.Errorf("invalid key accepted: %s", invalid)
		}
	}
}

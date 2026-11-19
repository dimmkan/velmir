package records

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildDeleteQuerySupportsCompositePrimaryKey(t *testing.T) {
	query, args, err := BuildDeleteQuery(`odd"schema`, mutationResource(), `{"id":"9","tenant_id":"7"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`DELETE FROM "odd""schema"."odd""table"`,
		`WHERE "tenant_id" = $1 AND "id" = $2`,
		`RETURNING "tenant_id", "id", "title"`,
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("query does not contain %q: %s", fragment, query)
		}
	}
	if want := []any{int64(7), int64(9)}; !reflect.DeepEqual(args, want) {
		t.Errorf("args = %#v, want %#v", args, want)
	}
}

func TestDeleteRejectsViewsMissingPrimaryKeysAndInvalidKeys(t *testing.T) {
	resource := mutationResource()
	resource.Kind = "view"
	_, _, err := BuildDeleteQuery("public", resource, `{"tenant_id":"7","id":"9"}`)
	assertMutationError(t, err, "RESOURCE_READ_ONLY", "")

	resource = mutationResource()
	resource.PrimaryKey = nil
	_, _, err = BuildDeleteQuery("public", resource, `{}`)
	assertMutationError(t, err, "RESOURCE_READ_ONLY", "")

	for _, key := range []string{`not-json`, `{}`, `{"id":"9"}`, `{"tenant_id":"7","id":"9","extra":"1"}`} {
		_, _, err = BuildDeleteQuery("public", mutationResource(), key)
		if err == nil {
			t.Errorf("invalid key accepted: %s", key)
		}
	}
}

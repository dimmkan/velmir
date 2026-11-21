package records

import (
	"strings"
	"testing"

	"velmir/backend/internal/introspection"
)

func TestEmptyReadableSetWithoutPrimaryKeyNeverSelectsWildcard(t *testing.T) {
	resource := introspection.Resource{Name: "items", Columns: []introspection.Column{{Name: "hidden", DataType: "text"}}}
	resource = resourceWithOnlyColumnsForTest(resource, nil)
	query, _ := BuildSelectQuery(resource, "public", ListOptions{Page: 1, Limit: 50, Direction: "asc"})
	if strings.Contains(query, "SELECT *") || strings.Contains(query, `"hidden"`) || !strings.Contains(query, "SELECT NULL") {
		t.Fatalf("unsafe empty readable query: %s", query)
	}
}

func resourceWithOnlyColumnsForTest(resource introspection.Resource, allowed []string) introspection.Resource {
	set := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		set[name] = true
	}
	columns := make([]introspection.Column, 0, len(allowed))
	for _, column := range resource.Columns {
		if set[column.Name] {
			columns = append(columns, column)
		}
	}
	resource.Columns = columns
	return resource
}

package records

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"velmir/backend/internal/introspection"
)

func testResource() introspection.Resource {
	return introspection.Resource{
		Name:       `odd"table`,
		Kind:       "table",
		PrimaryKey: []string{"id", "tenant_id"},
		Columns: []introspection.Column{
			{Name: "id", DataType: "bigint"},
			{Name: "tenant_id", DataType: "bigint"},
			{Name: "title", DataType: "character varying(255)"},
			{Name: "price", DataType: "numeric(12,2)"},
			{Name: "active", DataType: "boolean"},
			{Name: "published_on", DataType: "date"},
			{Name: "external_id", DataType: "uuid"},
			{Name: "payload", DataType: "json"},
		},
	}
}

func TestParseListOptionsDefaults(t *testing.T) {
	options, err := ParseListOptions(url.Values{}, testResource())
	if err != nil {
		t.Fatalf("ParseListOptions() error = %v", err)
	}
	if options.Page != 1 || options.Limit != DefaultLimit || options.Direction != "asc" {
		t.Errorf("options = %+v", options)
	}
	if options.Filters == nil || options.SearchColumns == nil {
		t.Error("empty options must be initialized")
	}
}

func TestBuildSelectQueryUsesMetadataParametersAndStablePagination(t *testing.T) {
	query := url.Values{
		"page":          {"3"},
		"limit":         {"2"},
		"sort":          {"title"},
		"direction":     {"desc"},
		"search":        {`100%_\`},
		"search_column": {"title"},
		"filter": {`[
			{"column":"price","operator":"gte","value":"12345678901234567890.01"},
			{"column":"active","operator":"eq","value":"true"},
			{"column":"published_on","operator":"is_null","value":false}
		]`},
	}
	options, err := ParseListOptions(query, testResource())
	if err != nil {
		t.Fatalf("ParseListOptions() error = %v", err)
	}

	sql, args := BuildSelectQuery(testResource(), `odd"schema`, options)

	for _, fragment := range []string{
		`FROM "odd""schema"."odd""table"`,
		`"price" >= $1::numeric`,
		`"active" = $2`,
		`"published_on" IS NOT NULL`,
		`"title" ILIKE $3 ESCAPE '\'`,
		`ORDER BY "title" DESC, "id" DESC, "tenant_id" DESC`,
		`LIMIT $4 OFFSET $5`,
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("query does not contain %q:\n%s", fragment, sql)
		}
	}
	wantArgs := []any{"12345678901234567890.01", true, int64(0), 3, int64(4)}
	wantArgs[2] = `%100\%\_\\%`
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestBuildSelectQueryDefaultsToPrimaryKey(t *testing.T) {
	options, err := ParseListOptions(url.Values{"limit": {"10"}}, testResource())
	if err != nil {
		t.Fatal(err)
	}
	sql, args := BuildSelectQuery(testResource(), "public", options)
	if !strings.Contains(sql, `ORDER BY "id" ASC, "tenant_id" ASC`) {
		t.Errorf("query is not stable: %s", sql)
	}
	if !reflect.DeepEqual(args, []any{11, int64(0)}) {
		t.Errorf("args = %#v", args)
	}
}

func TestParseListOptionsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		query url.Values
		code  string
	}{
		{name: "zero page", query: url.Values{"page": {"0"}}, code: "INVALID_PAGE"},
		{name: "huge page", query: url.Values{"page": {"9223372036854775808"}}, code: "INVALID_PAGE"},
		{name: "large limit", query: url.Values{"limit": {"101"}}, code: "INVALID_LIMIT"},
		{name: "duplicate limit", query: url.Values{"limit": {"10", "20"}}, code: "INVALID_LIMIT"},
		{name: "unknown sort", query: url.Values{"sort": {"missing"}}, code: "INVALID_SORT"},
		{name: "SQL in sort", query: url.Values{"sort": {"title; DROP TABLE books;--"}}, code: "INVALID_SORT"},
		{name: "JSON sort", query: url.Values{"sort": {"payload"}}, code: "INVALID_SORT"},
		{name: "direction SQL", query: url.Values{"direction": {"asc; DROP TABLE books"}}, code: "INVALID_DIRECTION"},
		{name: "unknown search column", query: url.Values{"search": {"x"}, "search_column": {"missing"}}, code: "INVALID_SEARCH"},
		{name: "numeric search column", query: url.Values{"search": {"x"}, "search_column": {"id"}}, code: "INVALID_SEARCH"},
		{name: "unknown filter column", query: url.Values{"filter": {`[{"column":"missing","operator":"eq","value":"x"}]`}}, code: "INVALID_FILTER"},
		{name: "filter SQL column", query: url.Values{"filter": {`[{"column":"title) OR 1=1 --","operator":"eq","value":"x"}]`}}, code: "INVALID_FILTER"},
		{name: "unknown operator", query: url.Values{"filter": {`[{"column":"title","operator":"matches","value":"x"}]`}}, code: "INVALID_FILTER"},
		{name: "invalid bigint", query: url.Values{"filter": {`[{"column":"id","operator":"eq","value":"1 OR true"}]`}}, code: "INVALID_FILTER"},
		{name: "invalid numeric", query: url.Values{"filter": {`[{"column":"price","operator":"eq","value":"1e999999999999999999999"}]`}}, code: "INVALID_FILTER"},
		{name: "invalid boolean", query: url.Values{"filter": {`[{"column":"active","operator":"eq","value":"yes"}]`}}, code: "INVALID_FILTER"},
		{name: "invalid date", query: url.Values{"filter": {`[{"column":"published_on","operator":"eq","value":"2024-02-30"}]`}}, code: "INVALID_FILTER"},
		{name: "invalid uuid", query: url.Values{"filter": {`[{"column":"external_id","operator":"eq","value":"not-a-uuid"}]`}}, code: "INVALID_FILTER"},
		{name: "JSON equality unsupported", query: url.Values{"filter": {`[{"column":"payload","operator":"eq","value":"{}"}]`}}, code: "INVALID_FILTER"},
		{name: "invalid null value", query: url.Values{"filter": {`[{"column":"payload","operator":"is_null","value":"true"}]`}}, code: "INVALID_FILTER"},
		{name: "malformed filter", query: url.Values{"filter": {"not-json"}}, code: "INVALID_FILTER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseListOptions(tt.query, testResource())
			validationErr, ok := err.(*ValidationError)
			if !ok {
				t.Fatalf("error = %T %v, want ValidationError", err, err)
			}
			if validationErr.Code != tt.code {
				t.Errorf("code = %q, want %q", validationErr.Code, tt.code)
			}
		})
	}
}

func TestContainsEscapesLikeWildcards(t *testing.T) {
	options, err := ParseListOptions(url.Values{
		"filter": {`[{"column":"title","operator":"contains","value":"50%_\\"}]`},
	}, testResource())
	if err != nil {
		t.Fatal(err)
	}
	_, args := BuildSelectQuery(testResource(), "public", options)
	if got, want := args[0], `%50\%\_\\%`; got != want {
		t.Errorf("pattern = %q, want %q", got, want)
	}
}

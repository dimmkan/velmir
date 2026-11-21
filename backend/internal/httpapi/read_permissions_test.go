package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"velmir/backend/internal/audit"
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

func readFieldTestPermissions(columns []string) authorization.Permissions {
	return authorization.Permissions{{
		Schema: "public", Resource: "items", Operations: []authorization.Operation{authorization.Read},
		FieldPermissions: []authorization.FieldPermission{{Action: authorization.Read, Mode: authorization.FieldAllowlist, Columns: columns}},
	}}
}

func TestReadPermissionsRestrictListSQLAndQueryOptions(t *testing.T) {
	for _, target := range []string{
		"/api/resources/public/items/records?sort=name",
		"/api/resources/public/items/records?search=secret&search_column=name",
		"/api/resources/public/items/records?search=secret&search_column=stamp",
		`/api/resources/public/items/records?filter=%5B%7B%22column%22%3A%22name%22%2C%22operator%22%3A%22contains%22%2C%22value%22%3A%22secret%22%7D%5D`,
	} {
		called := false
		lister := recordListerFunc(func(context.Context, string, introspection.Resource, records.ListOptions) (records.ListResult, error) {
			called = true
			return records.ListResult{}, nil
		})
		response := httptest.NewRecorder()
		inspector := schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) { return fieldPermissionCatalog(), nil })
		router := NewRouter(nil, nil, inspector, lister, nil, testAuthService{user: testUser()}, testPermissionProvider{permissions: readFieldTestPermissions([]string{"note"})}, nil, false)
		router.ServeHTTP(response, requestWithSession(httptest.NewRequest(http.MethodGet, target, nil)))
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "FIELD_NOT_READABLE") {
			t.Fatalf("%s = %d %s", target, response.Code, response.Body.String())
		}
		if called {
			t.Fatalf("forbidden query reached lister: %s", target)
		}
	}

	var listed introspection.Resource
	var options records.ListOptions
	lister := recordListerFunc(func(_ context.Context, _ string, resource introspection.Resource, got records.ListOptions) (records.ListResult, error) {
		listed, options = resource, got
		query, _ := records.BuildSelectQuery(resource, "public", got)
		if strings.Contains(query, `"name"`) || strings.Contains(query, `"required_value"`) || strings.Contains(query, `"stamp"`) {
			t.Fatalf("hidden columns in SELECT: %s", query)
		}
		return records.ListResult{Records: []map[string]any{{"id": "1", "note": "visible", "name": "must be filtered"}}, Page: 1, Limit: 50}, nil
	})
	router := NewRouter(nil, nil, schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) { return fieldPermissionCatalog(), nil }), lister, nil, testAuthService{user: testUser()}, testPermissionProvider{permissions: readFieldTestPermissions([]string{"note"})}, nil, false)
	response := httptest.NewRecorder()
	request := requestWithSession(httptest.NewRequest(http.MethodGet, "/api/resources/public/items/records?search=visible", nil))
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !reflect.DeepEqual(options.SearchColumns, []string{"note"}) {
		t.Fatalf("implicit search = %d %s, options = %+v", response.Code, response.Body.String(), options)
	}
	if len(listed.Columns) != 2 || listed.Columns[0].Name != "id" || listed.Columns[1].Name != "note" {
		t.Fatalf("list metadata = %+v", listed.Columns)
	}
	if strings.Contains(response.Body.String(), "name") {
		t.Fatalf("list response contains hidden field: %s", response.Body.String())
	}
}

func TestReadPermissionsSchemaPrimaryKeyAndWriteOnly(t *testing.T) {
	permissions := readFieldTestPermissions([]string{})
	permissions[0].Operations = []authorization.Operation{authorization.Read, authorization.Create, authorization.Update}
	permissions[0].FieldPermissions = append(permissions[0].FieldPermissions,
		authorization.FieldPermission{Action: authorization.Create, Mode: authorization.FieldAllowlist, Columns: []string{"name", "required_value"}},
		authorization.FieldPermission{Action: authorization.Update, Mode: authorization.FieldAllowlist, Columns: []string{"note"}},
	)
	response := httptest.NewRecorder()
	newFieldTestRouter(permissions, recordWriterFunc{}, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/schema", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("schema = %d %s", response.Code, response.Body.String())
	}
	var catalog introspection.Catalog
	if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	resource := catalog.Schemas[0].Resources[0]
	if !reflect.DeepEqual(resource.ReadableFields, []string{"id"}) || !reflect.DeepEqual(resource.WriteOnlyFields, []string{"name", "required_value", "note"}) {
		t.Fatalf("read/write-only fields = %+v %+v", resource.ReadableFields, resource.WriteOnlyFields)
	}
	for _, column := range resource.Columns {
		if column.Name == "id" && !column.PrimaryKey {
			t.Fatal("PK metadata was lost")
		}
		if column.Name == "generated_value" || column.Name == "stamp" {
			t.Fatalf("hidden non-writable column exposed: %+v", column)
		}
	}
}

func TestWriteOnlyMutationResponsesStayFilteredWhileAuditKeepsActualValues(t *testing.T) {
	permissions := fieldTestPermissions([]string{"name", "required_value", "note"}, []string{"note"}, false)
	permissions[0].FieldPermissions = append(permissions[0].FieldPermissions,
		authorization.FieldPermission{Action: authorization.Read, Mode: authorization.FieldAllowlist, Columns: []string{"name"}})
	writer := recordWriterFunc{
		create: func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error) {
			return map[string]any{"id": "1", "name": "visible", "required_value": "required secret", "note": "write-only secret"}, nil
		},
		update: func(context.Context, string, introspection.Resource, string, records.RecordValues) (records.UpdateResult, error) {
			return records.UpdateResult{Before: map[string]any{"id": "1", "note": "old secret"}, After: map[string]any{"id": "1", "name": "visible", "note": "new secret"}}, nil
		},
	}
	store := &testAuditStore{}
	router := newFieldTestRouter(permissions, writer, store)
	for _, test := range []struct{ method, target, body string }{
		{http.MethodPost, "/api/resources/public/items/records", `{"values":{"name":"visible","required_value":"required secret","note":"write-only secret"}}`},
		{http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, `{"values":{"note":"new secret"}}`},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(test.method, test.target, strings.NewReader(test.body)))
		if response.Code >= 300 || strings.Contains(response.Body.String(), "secret") || !strings.Contains(response.Body.String(), `"id":"1"`) {
			t.Fatalf("mutation response = %d %s", response.Code, response.Body.String())
		}
	}
	if len(store.inputs) != 2 || store.inputs[0].After["note"] != "write-only secret" || store.inputs[1].Before["note"] != "old secret" {
		t.Fatalf("internal audit lost actual values: %+v", store.inputs)
	}
}

func TestHistoryFiltersCurrentReadFieldsWithoutChangingPaginationOrNulls(t *testing.T) {
	store := &testAuditStore{history: audit.HistoryResult{Page: 2, Limit: 1, HasNext: true, Events: []audit.Event{
		{ID: "1", Action: audit.Update, RecordKey: json.RawMessage(`{"id":"1"}`), Before: json.RawMessage(`{"id":"1","name":"before","note":"hidden before"}`), After: json.RawMessage(`{"id":"1","name":"after","note":"hidden after"}`)},
		{ID: "2", Action: audit.Create, RecordKey: json.RawMessage(`{"id":"1"}`), Before: nil, After: json.RawMessage(`{"id":"1","note":"hidden create"}`)},
	}}}
	response := httptest.NewRecorder()
	newFieldTestRouter(readFieldTestPermissions([]string{"name"}), nil, store).ServeHTTP(response, requestWithSession(httptest.NewRequest(http.MethodGet, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D/history?page=2&limit=1`, nil)))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "hidden") || strings.Contains(response.Body.String(), `"note"`) {
		t.Fatalf("filtered history = %d %s", response.Code, response.Body.String())
	}
	for _, fragment := range []string{`"name":"before"`, `"name":"after"`, `"id":"1"`, `"page":2`, `"has_next":true`, `"before":null`} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Errorf("history missing %s: %s", fragment, response.Body.String())
		}
	}
	if !strings.Contains(string(store.history.Events[0].Before), "hidden before") {
		t.Fatal("stored history test fixture was modified")
	}
}

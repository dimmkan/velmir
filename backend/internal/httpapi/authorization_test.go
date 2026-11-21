package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

func newRBACTestRouter(permissions authorization.Permissions, inspector schemaInspector, lister recordLister, writer recordWriter) http.Handler {
	router := NewRouter(nil, nil, inspector, lister, writer, testAuthService{user: testUser()}, testPermissionProvider{permissions: permissions}, nil, false)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
			r.Header.Set(csrfHeaderName, testCSRFToken)
		}
		router.ServeHTTP(w, r)
	})
}

func TestUserWithoutRoleGets403ButMeReturnsEmptyPermissions(t *testing.T) {
	inspectorCalled := false
	inspector := schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) {
		inspectorCalled = true
		return recordsCatalog(), nil
	})
	router := newRBACTestRouter(make(authorization.Permissions, 0), inspector, nil, nil)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/schema", nil),
		httptest.NewRequest(http.MethodGet, "/api/resources/public/items/records", nil),
		httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{}}`)),
		httptest.NewRequest(http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, strings.NewReader(`{"values":{"name":"x"}}`)),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"FORBIDDEN"`) {
			t.Errorf("%s %s status = %d, body = %s", request.Method, request.URL.Path, response.Code, response.Body.String())
		}
	}
	if inspectorCalled {
		t.Fatal("introspection ran without resource permission")
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	var body authResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(body.Permissions) != 0 {
		t.Fatalf("me response = %d %+v", response.Code, body)
	}
}

func TestViewerCanReadButCannotMutateWithValidCSRF(t *testing.T) {
	mutationCalled := false
	writer := recordWriterFunc{
		create: func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error) {
			mutationCalled = true
			return nil, nil
		},
		update: func(context.Context, string, introspection.Resource, string, records.RecordValues) (records.UpdateResult, error) {
			mutationCalled = true
			return records.UpdateResult{}, nil
		},
	}
	lister := recordListerFunc(func(context.Context, string, introspection.Resource, records.ListOptions) (records.ListResult, error) {
		return records.ListResult{Records: []map[string]any{}, Page: 1, Limit: 50}, nil
	})
	router := newRBACTestRouter(viewerTestPermissions(), recordsInspector(), lister, writer)

	schemaResponse := httptest.NewRecorder()
	router.ServeHTTP(schemaResponse, httptest.NewRequest(http.MethodGet, "/api/schema", nil))
	var catalog introspection.Catalog
	if err := json.NewDecoder(schemaResponse.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	if schemaResponse.Code != http.StatusOK || catalog.WriteEnabled {
		t.Fatalf("viewer schema = %d %+v", schemaResponse.Code, catalog)
	}

	recordsResponse := httptest.NewRecorder()
	router.ServeHTTP(recordsResponse, httptest.NewRequest(http.MethodGet, "/api/resources/public/items/records", nil))
	if recordsResponse.Code != http.StatusOK {
		t.Fatalf("viewer records status = %d, body = %s", recordsResponse.Code, recordsResponse.Body.String())
	}

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{"name":"x"}}`)),
		httptest.NewRequest(http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, strings.NewReader(`{"values":{"name":"x"}}`)),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"FORBIDDEN"`) {
			t.Errorf("viewer mutation status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	if mutationCalled {
		t.Fatal("viewer mutation reached the writer")
	}
}

func TestAdminCanReadCreateUpdateAndDelete(t *testing.T) {
	writer := recordWriterFunc{
		create: func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error) {
			return map[string]any{"id": "1"}, nil
		},
		update: func(context.Context, string, introspection.Resource, string, records.RecordValues) (records.UpdateResult, error) {
			return records.UpdateResult{After: map[string]any{"id": "1"}}, nil
		},
		delete: func(context.Context, string, introspection.Resource, string) (map[string]any, error) {
			return map[string]any{"id": "1"}, nil
		},
	}
	lister := recordListerFunc(func(context.Context, string, introspection.Resource, records.ListOptions) (records.ListResult, error) {
		return records.ListResult{Records: []map[string]any{}, Page: 1, Limit: 50}, nil
	})
	router := newRBACTestRouter(adminTestPermissions(), recordsInspector(), lister, writer)

	for _, test := range []struct {
		method string
		target string
		body   string
		want   int
	}{
		{method: http.MethodGet, target: "/api/resources/public/items/records", want: http.StatusOK},
		{method: http.MethodPost, target: "/api/resources/public/items/records", body: `{"values":{}}`, want: http.StatusCreated},
		{method: http.MethodPatch, target: `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, body: `{"values":{"name":"x"}}`, want: http.StatusOK},
		{method: http.MethodDelete, target: `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, want: http.StatusNoContent},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(test.method, test.target, strings.NewReader(test.body)))
		if response.Code != test.want {
			t.Errorf("admin %s status = %d, want %d; body = %s", test.method, response.Code, test.want, response.Body.String())
		}
	}
}

func TestSchemaHidesResourcesWithoutReadPermission(t *testing.T) {
	permissions := authorization.EffectivePermissions([]authorization.Grant{
		{Schema: "public", Resource: "books", Operation: authorization.Read},
		{Schema: "public", Resource: "books", Operation: authorization.Create},
	})
	catalog := introspection.Catalog{Schemas: []introspection.Schema{
		{Name: "public", Resources: []introspection.Resource{
			{Name: "books", Kind: "table", ForeignKeys: []introspection.ForeignKey{{ReferencedSchema: "private", ReferencedTable: "secrets"}}},
			{Name: "notes"},
		}},
		{Name: "private", Resources: []introspection.Resource{{Name: "secrets"}}},
	}}
	inspector := schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) { return catalog, nil })
	response := httptest.NewRecorder()
	newRBACTestRouter(permissions, inspector, nil, recordWriterFunc{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/schema", nil))

	var got introspection.Catalog
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(got.Schemas) != 1 || len(got.Schemas[0].Resources) != 1 || got.Schemas[0].Resources[0].Name != "books" {
		t.Fatalf("filtered schema = %d %+v, body = %s", response.Code, got, response.Body.String())
	}
	if !got.WriteEnabled || len(got.Schemas[0].Resources[0].ForeignKeys) != 0 {
		t.Fatalf("schema leaks an inaccessible resource or has incorrect write state: %+v", got)
	}

	for _, target := range []string{"/api/resources/public/notes/records", "/api/resources/private/secrets/records"} {
		response := httptest.NewRecorder()
		newRBACTestRouter(permissions, recordsInspector(), nil, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusForbidden {
			t.Errorf("%s status = %d, want 403", target, response.Code)
		}
	}
}

func TestMeReturnsEffectivePermissions(t *testing.T) {
	permissions := authorization.EffectivePermissions([]authorization.Grant{
		{Schema: "public", Resource: "books", Operation: authorization.Read},
		{Schema: "public", Resource: "books", Operation: authorization.Update},
	})
	response := httptest.NewRecorder()
	newRBACTestRouter(permissions, nil, nil, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	var body authResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(body.Permissions) != 1 || !body.Permissions.Allows("public", "books", authorization.Update) || body.Permissions.Allows("public", "books", authorization.Create) {
		t.Fatalf("me permissions = %+v", body.Permissions)
	}
}

func TestAuthorizationFailureDoesNotLeakInternals(t *testing.T) {
	router := NewRouter(nil, nil, recordsInspector(), nil, nil, testAuthService{user: testUser()}, testPermissionProvider{err: errors.New("database details")}, nil, false)
	request := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "database details") {
		t.Fatalf("authorization failure = %d %s", response.Code, response.Body.String())
	}

}

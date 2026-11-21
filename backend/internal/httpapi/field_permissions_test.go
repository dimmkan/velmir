package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"velmir/backend/internal/auth"
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

func fieldPermissionCatalog() introspection.Catalog {
	idDefault := "nextval('items_id_seq'::regclass)"
	stampDefault := "now()"
	return introspection.Catalog{Schemas: []introspection.Schema{{Name: "public", Resources: []introspection.Resource{{
		Name: "items", Kind: "table", PrimaryKey: []string{"id"},
		Columns: []introspection.Column{
			{Name: "id", DataType: "bigint", PrimaryKey: true, Default: &idDefault},
			{Name: "name", DataType: "text", Editable: true},
			{Name: "required_value", DataType: "text", Editable: true},
			{Name: "note", DataType: "text", Nullable: true, Editable: true},
			{Name: "stamp", DataType: "timestamp with time zone", Nullable: true, Editable: true, Default: &stampDefault},
			{Name: "generated_value", DataType: "text", Nullable: true, Generated: true},
		},
		ForeignKeys: []introspection.ForeignKey{}, ReverseRelations: []introspection.ReverseRelation{},
	}}}}}
}

func fieldTestPermissions(createColumns, updateColumns []string, unrestricted bool) authorization.Permissions {
	mode := authorization.FieldAllowlist
	if unrestricted {
		mode = authorization.FieldUnrestricted
	}
	return authorization.Permissions{{
		Schema: "public", Resource: "items", Operations: []authorization.Operation{authorization.Read, authorization.Create, authorization.Update},
		FieldPermissions: []authorization.FieldPermission{
			{Action: authorization.Create, Mode: mode, Columns: createColumns},
			{Action: authorization.Update, Mode: mode, Columns: updateColumns},
		},
	}}
}

func newFieldTestRouter(permissions authorization.Permissions, writer recordWriter, auditStore recordAudit) http.Handler {
	inspector := schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) { return fieldPermissionCatalog(), nil })
	router := NewRouter(nil, nil, inspector, nil, writer, testAuthService{user: testUser()}, testPermissionProvider{permissions: permissions}, auditStore, false)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
			r.Header.Set(csrfHeaderName, testCSRFToken)
		}
		router.ServeHTTP(w, r)
	})
}

func TestSchemaEffectiveFieldsAndImpossibleCreate(t *testing.T) {
	permissions := fieldTestPermissions([]string{"name"}, []string{"name"}, false)
	response := httptest.NewRecorder()
	newFieldTestRouter(permissions, recordWriterFunc{}, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/schema", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("schema status = %d, body = %s", response.Code, response.Body.String())
	}
	var catalog introspection.Catalog
	if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	resource := catalog.Schemas[0].Resources[0]
	if len(resource.CreatableFields) != 1 || resource.CreatableFields[0] != "name" || resource.CanCreate || resource.CreateDisabledReason != createDisabledRequired {
		t.Errorf("create metadata = %+v", resource)
	}
	if len(resource.UpdatableFields) != 1 || resource.UpdatableFields[0] != "name" || !resource.CanUpdate || !catalog.WriteEnabled {
		t.Errorf("update metadata = %+v, write_enabled = %v", resource, catalog.WriteEnabled)
	}
	if len(resource.Columns) != 6 {
		t.Errorf("read columns changed: %+v", resource.Columns)
	}
}

func TestFieldForbiddenAndMixedMutationsNeverReachWriterOrAudit(t *testing.T) {
	calls := 0
	writer := recordWriterFunc{
		create: func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error) {
			calls++
			return map[string]any{"id": "1"}, nil
		},
		update: func(context.Context, string, introspection.Resource, string, records.RecordValues) (records.UpdateResult, error) {
			calls++
			return records.UpdateResult{Before: map[string]any{"id": "1"}, After: map[string]any{"id": "1"}}, nil
		},
	}
	auditStore := &testAuditStore{}
	permissions := fieldTestPermissions([]string{"name", "required_value"}, []string{"name"}, false)
	router := newFieldTestRouter(permissions, writer, auditStore)
	for _, test := range []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, "/api/resources/public/items/records", `{"values":{"note":"forbidden"}}`},
		{http.MethodPost, "/api/resources/public/items/records", `{"values":{"name":"allowed","required_value":"yes","note":"forbidden"}}`},
		{http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, `{"values":{"note":"forbidden"}}`},
		{http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, `{"values":{"name":"allowed","note":"forbidden"}}`},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(test.method, test.target, strings.NewReader(test.body)))
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"FIELD_FORBIDDEN"`) {
			t.Errorf("%s %s response = %d %s", test.method, test.body, response.Code, response.Body.String())
		}
	}
	if calls != 0 || len(auditStore.inputs) != 0 {
		t.Fatalf("forbidden mutations reached writer %d times or audit %d times", calls, len(auditStore.inputs))
	}
}

func TestAllowlistMutationsAndMetadataValidationPrecedence(t *testing.T) {
	calls := 0
	writer := recordWriterFunc{
		create: func(_ context.Context, _ string, _ introspection.Resource, values records.RecordValues) (map[string]any, error) {
			calls++
			if len(values) != 2 || string(values["name"]) != `"new"` || string(values["required_value"]) != `"required"` {
				t.Errorf("create values = %#v", values)
			}
			return map[string]any{"id": "1", "name": "new"}, nil
		},
		update: func(_ context.Context, _ string, _ introspection.Resource, _ string, values records.RecordValues) (records.UpdateResult, error) {
			calls++
			if len(values) != 1 || string(values["name"]) != `"updated"` {
				t.Errorf("update values = %#v", values)
			}
			return records.UpdateResult{Before: map[string]any{"id": "1", "name": "old"}, After: map[string]any{"id": "1", "name": "updated"}}, nil
		},
	}
	auditStore := &testAuditStore{}
	permissions := fieldTestPermissions([]string{"name", "required_value"}, []string{"name"}, false)
	router := newFieldTestRouter(permissions, writer, auditStore)
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{"name":"new","required_value":"required"}}`)))
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("allowlist create = %d %s", createResponse.Code, createResponse.Body.String())
	}
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, httptest.NewRequest(http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, strings.NewReader(`{"values":{"name":"updated"}}`)))
	if updateResponse.Code != http.StatusOK || calls != 2 || len(auditStore.inputs) != 2 {
		t.Fatalf("allowlist update = %d %s, calls = %d, audit = %d", updateResponse.Code, updateResponse.Body.String(), calls, len(auditStore.inputs))
	}

	for _, body := range []string{`{"values":{"id":"2"}}`, `{"values":{"generated_value":"x"}}`, `{"values":{"unknown":"x"}}`} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, strings.NewReader(body)))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_RECORD"`) {
			t.Errorf("metadata validation %s = %d %s", body, response.Code, response.Body.String())
		}
	}
}

func TestEmptyAllowlistAndDefaultOnlyCreate(t *testing.T) {
	permissions := fieldTestPermissions([]string{}, []string{}, false)
	writer := recordWriterFunc{}
	router := newFieldTestRouter(permissions, writer, nil)
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{}}`)))
	if createResponse.Code != http.StatusForbidden || !strings.Contains(createResponse.Body.String(), `"code":"CREATE_IMPOSSIBLE"`) {
		t.Fatalf("impossible create = %d %s", createResponse.Code, createResponse.Body.String())
	}
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, httptest.NewRequest(http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, strings.NewReader(`{"values":{}}`)))
	if updateResponse.Code != http.StatusForbidden || !strings.Contains(updateResponse.Body.String(), `"code":"NO_WRITABLE_FIELDS"`) {
		t.Fatalf("empty update = %d %s", updateResponse.Code, updateResponse.Body.String())
	}

	catalog := fieldPermissionCatalog()
	resource := &catalog.Schemas[0].Resources[0]
	resource.Columns = []introspection.Column{{Name: "stamp", DataType: "text", Editable: true, Default: stringPointer("default")}}
	applyFieldPermissions("public", resource, fieldTestPermissions([]string{}, []string{}, false), true)
	if !resource.CanCreate || len(resource.CreatableFields) != 0 {
		t.Fatalf("default-only create with empty allowlist = %+v", resource)
	}
}

func stringPointer(value string) *string { return &value }

func TestPermissionAPIValidatesFieldAllowlistsAgainstMetadata(t *testing.T) {
	inspector := schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) { return fieldPermissionCatalog(), nil })
	for _, test := range []struct {
		name       string
		body       string
		serviceErr error
		wantStatus int
		wantCalls  int
	}{
		{name: "valid", body: `{"permissions":[{"schema":"public","resource":"items","operations":["update"],"field_permissions":[{"action":"update","mode":"allowlist","columns":["name"]}]}]}`, wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "empty", body: `{"permissions":[{"schema":"public","resource":"items","operations":["update"],"field_permissions":[{"action":"update","mode":"allowlist","columns":[]}]}]}`, wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "primary key", body: `{"permissions":[{"schema":"public","resource":"items","operations":["update"],"field_permissions":[{"action":"update","mode":"allowlist","columns":["id"]}]}]}`, wantStatus: http.StatusBadRequest},
		{name: "generated", body: `{"permissions":[{"schema":"public","resource":"items","operations":["update"],"field_permissions":[{"action":"update","mode":"allowlist","columns":["generated_value"]}]}]}`, wantStatus: http.StatusBadRequest},
		{name: "unknown", body: `{"permissions":[{"schema":"public","resource":"items","operations":["update"],"field_permissions":[{"action":"update","mode":"allowlist","columns":["missing"]}]}]}`, wantStatus: http.StatusBadRequest},
		{name: "duplicate", body: `{"permissions":[{"schema":"public","resource":"items","operations":["update"],"field_permissions":[{"action":"update","mode":"allowlist","columns":["name","name"]}]}]}`, wantStatus: http.StatusBadRequest},
		{name: "missing role", body: `{"permissions":[{"schema":"public","resource":"items","operations":["update"],"field_permissions":[{"action":"update","mode":"allowlist","columns":["name"]}]}]}`, serviceErr: auth.ErrRoleNotFound, wantStatus: http.StatusNotFound, wantCalls: 1},
		{name: "system role", body: `{"permissions":[{"schema":"public","resource":"items","operations":["update"],"field_permissions":[{"action":"update","mode":"allowlist","columns":["name"]}]}]}`, serviceErr: auth.ErrSystemRoleMutable, wantStatus: http.StatusConflict, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeAdministrationService{err: test.serviceErr}
			router := NewRouter(nil, nil, inspector, nil, nil, testAuthService{user: testUser()}, testPermissionProvider{permissions: adminTestPermissions(), isAdmin: true}, nil, false, service)
			request := httptest.NewRequest(http.MethodPut, "/api/admin/roles/3/permissions", strings.NewReader(test.body))
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
			request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
			request.Header.Set(csrfHeaderName, testCSRFToken)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			calls := 0
			for _, call := range service.calls {
				if call == "permissions" {
					calls++
				}
			}
			if calls != test.wantCalls {
				t.Fatalf("service calls = %d, want %d", calls, test.wantCalls)
			}
		})
	}
}

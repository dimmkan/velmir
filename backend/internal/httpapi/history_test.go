package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"velmir/backend/internal/audit"
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

type testAuditStore struct {
	inputs       []audit.Input
	recordErr    error
	history      audit.HistoryResult
	historyErr   error
	historyCalls int
	schema       string
	resource     string
	key          json.RawMessage
	page         int64
	limit        int
}

func (s *testAuditStore) Record(_ context.Context, input audit.Input) error {
	s.inputs = append(s.inputs, input)
	return s.recordErr
}

func (s *testAuditStore) History(_ context.Context, schema, resource string, key json.RawMessage, page int64, limit int) (audit.HistoryResult, error) {
	s.historyCalls++
	s.schema, s.resource, s.key, s.page, s.limit = schema, resource, key, page, limit
	return s.history, s.historyErr
}

func newAuditTestRouter(authService authService, permissions authorization.Permissions, writer recordWriter, auditStore recordAudit) http.Handler {
	return NewRouter(nil, nil, recordsInspector(), nil, writer, authService, testPermissionProvider{permissions: permissions}, auditStore, false)
}

func auditMutationRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	request.Header.Set(csrfHeaderName, testCSRFToken)
	return request
}

func TestMutationsRecordAuthenticatedActorAndActualValues(t *testing.T) {
	store := &testAuditStore{}
	writer := recordWriterFunc{
		create: func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error) {
			return map[string]any{"id": "9223372036854775807", "name": "database default"}, nil
		},
		update: func(context.Context, string, introspection.Resource, string, records.RecordValues) (records.UpdateResult, error) {
			return records.UpdateResult{
				Before: map[string]any{"id": "1", "name": "actual before"},
				After:  map[string]any{"id": "1", "name": "actual after"},
			}, nil
		},
	}
	router := newAuditTestRouter(testAuthService{user: testUser()}, adminTestPermissions(), writer, store)

	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, auditMutationRequest(http.MethodPost, "/api/resources/public/items/records", `{"values":{"name":"client value"}}`))
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createResponse.Code, createResponse.Body.String())
	}
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, auditMutationRequest(http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, `{"values":{"name":"client after"}}`))
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	if len(store.inputs) != 2 {
		t.Fatalf("audit events = %d", len(store.inputs))
	}
	created := store.inputs[0]
	if created.ActorUserID != testUser().ID || created.Action != audit.Create || created.Before != nil || created.After["name"] != "database default" || string(created.RecordKey) != `{"id":"9223372036854775807"}` {
		t.Errorf("create audit = %+v", created)
	}
	updated := store.inputs[1]
	if updated.ActorUserID != testUser().ID || updated.Action != audit.Update || updated.Before["name"] != "actual before" || updated.After["name"] != "actual after" {
		t.Errorf("update audit = %+v", updated)
	}
}

func TestFailedMutationAndCommittedAuditFailure(t *testing.T) {
	store := &testAuditStore{}
	writer := recordWriterFunc{create: func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error) {
		return nil, errors.New("target failed")
	}}
	response := httptest.NewRecorder()
	newAuditTestRouter(testAuthService{user: testUser()}, adminTestPermissions(), writer, store).ServeHTTP(response, auditMutationRequest(http.MethodPost, "/api/resources/public/items/records", `{"values":{}}`))
	if response.Code != http.StatusInternalServerError || len(store.inputs) != 0 {
		t.Fatalf("failed mutation response = %d, audit events = %d", response.Code, len(store.inputs))
	}

	calls := 0
	store = &testAuditStore{recordErr: errors.New("admin unavailable with secret connection string")}
	writer = recordWriterFunc{create: func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error) {
		calls++
		return map[string]any{"id": "9", "name": "committed"}, nil
	}}
	response = httptest.NewRecorder()
	newAuditTestRouter(testAuthService{user: testUser()}, adminTestPermissions(), writer, store).ServeHTTP(response, auditMutationRequest(http.MethodPost, "/api/resources/public/items/records", `{"values":{}}`))
	if response.Code != http.StatusInternalServerError || calls != 1 {
		t.Fatalf("audit failure status = %d, target calls = %d", response.Code, calls)
	}
	var body struct {
		Error  apiError       `json:"error"`
		Record map[string]any `json:"record"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "AUDIT_WRITE_FAILED" || !body.Error.MutationCommitted || body.Record["name"] != "committed" {
		t.Errorf("audit failure body = %+v", body)
	}
	if strings.Contains(response.Body.String(), "secret connection string") {
		t.Fatal("audit failure leaked internal error")
	}
}

func TestRecordHistoryResponseAndPagination(t *testing.T) {
	now := time.Now().UTC()
	store := &testAuditStore{history: audit.HistoryResult{
		Events: []audit.Event{{
			ID: "00112233-4455-6677-8899-aabbccddeeff", Actor: audit.Actor{ID: 1, Email: "admin@example.test", DisplayName: "Administrator"},
			Timestamp: now, Action: audit.Update, Schema: "public", Resource: "items",
			RecordKey: json.RawMessage(`{"id":"1"}`), Before: json.RawMessage(`{"name":"before"}`), After: json.RawMessage(`{"name":"after","big":"9223372036854775807"}`),
		}},
		Page: 2, Limit: 1, HasNext: true,
	}}
	request := httptest.NewRequest(http.MethodGet, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D/history?page=2&limit=1`, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
	response := httptest.NewRecorder()
	newAuditTestRouter(testAuthService{user: testUser()}, viewerTestPermissions(), nil, store).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.historyCalls != 1 || store.schema != "public" || store.resource != "items" || string(store.key) != `{"id":"1"}` || store.page != 2 || store.limit != 1 {
		t.Errorf("history query = %+v", store)
	}
	for _, fragment := range []string{`"events":[`, `"action":"update"`, `"name":"before"`, `"big":"9223372036854775807"`, `"page":2`, `"has_next":true`} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Errorf("response does not contain %s: %s", fragment, response.Body.String())
		}
	}
}

func TestRecordHistoryEmptyValidationAndFailures(t *testing.T) {
	store := &testAuditStore{history: audit.HistoryResult{Events: []audit.Event{}, Page: 1, Limit: 20}}
	request := httptest.NewRequest(http.MethodGet, `/api/resources/public/items/records/%7B%22id%22%3A%229%22%7D/history`, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
	response := httptest.NewRecorder()
	router := newAuditTestRouter(testAuthService{user: testUser()}, viewerTestPermissions(), nil, store)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"events":[]`) || store.limit != defaultHistoryLimit {
		t.Fatalf("empty history = %d %s", response.Code, response.Body.String())
	}

	for _, target := range []string{
		`/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D/history?page=0`,
		`/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D/history?limit=101`,
		`/api/resources/public/items/records/not-json/history`,
		`/api/resources/public/items/records/%7B%22unknown%22%3A%221%22%7D/history`,
	} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, requestWithSession(httptest.NewRequest(http.MethodGet, target, nil)))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d", target, response.Code)
		}
	}

	store.historyErr = errors.New("secret database error")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, requestWithSession(httptest.NewRequest(http.MethodGet, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D/history`, nil)))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret") {
		t.Errorf("history failure = %d %s", response.Code, response.Body.String())
	}
}

func TestRecordHistoryRequiresSessionAndReadPermission(t *testing.T) {
	store := &testAuditStore{}
	router := newAuditTestRouter(testAuthService{user: testUser()}, viewerTestPermissions(), nil, store)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D/history`, nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", response.Code)
	}

	forbiddenStore := &testAuditStore{}
	forbidden := newAuditTestRouter(testAuthService{user: testUser()}, make(authorization.Permissions, 0), nil, forbiddenStore)
	for _, target := range []string{
		`/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D/history`,
		`/api/resources/private/secrets/records/%7B%22id%22%3A%221%22%7D/history`,
	} {
		response = httptest.NewRecorder()
		forbidden.ServeHTTP(response, requestWithSession(httptest.NewRequest(http.MethodGet, target, nil)))
		if response.Code != http.StatusForbidden {
			t.Errorf("forbidden history status = %d", response.Code)
		}
	}
	if forbiddenStore.historyCalls != 0 {
		t.Fatal("inaccessible history reached audit storage")
	}
}

func requestWithSession(request *http.Request) *http.Request {
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
	return request
}

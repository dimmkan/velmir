package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"velmir/backend/internal/audit"
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

const deleteTestPath = `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`

func TestDeleteRecordReturnsNoContentAndAuditsActualBeforeValues(t *testing.T) {
	store := &testAuditStore{}
	deleteCalls := 0
	writer := recordWriterFunc{delete: func(_ context.Context, schema string, resource introspection.Resource, key string) (map[string]any, error) {
		deleteCalls++
		if schema != "public" || resource.Name != "items" || key != `{"id":"1"}` {
			t.Errorf("delete arguments = %q, %q, %q", schema, resource.Name, key)
		}
		return map[string]any{"id": "1", "name": "actual database value"}, nil
	}}
	response := httptest.NewRecorder()
	newAuditTestRouter(testAuthService{user: testUser()}, adminTestPermissions(), writer, store).ServeHTTP(response, auditMutationRequest(http.MethodDelete, deleteTestPath, ""))

	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("delete response = %d %q", response.Code, response.Body.String())
	}
	if deleteCalls != 1 || len(store.inputs) != 1 {
		t.Fatalf("delete calls = %d, audit events = %d", deleteCalls, len(store.inputs))
	}
	event := store.inputs[0]
	if event.ActorUserID != testUser().ID || event.Action != audit.Delete || event.Before["name"] != "actual database value" || event.After != nil || string(event.RecordKey) != `{"id":"1"}` {
		t.Errorf("delete audit = %+v", event)
	}
}

func TestDeleteFailuresDoNotCreateAudit(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "missing", err: &records.MutationError{Code: "RECORD_NOT_FOUND", Message: "Record was not found"}, status: http.StatusNotFound, code: "RECORD_NOT_FOUND"},
		{name: "constraint", err: &records.MutationError{Code: "FOREIGN_KEY_VIOLATION", Message: "The record is still referenced"}, status: http.StatusBadRequest, code: "FOREIGN_KEY_VIOLATION"},
		{name: "database", err: errors.New("DELETE secret SQL; stack trace"), status: http.StatusInternalServerError, code: "RECORD_MUTATION_FAILED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &testAuditStore{}
			writer := recordWriterFunc{delete: func(context.Context, string, introspection.Resource, string) (map[string]any, error) {
				return nil, test.err
			}}
			response := httptest.NewRecorder()
			newAuditTestRouter(testAuthService{user: testUser()}, adminTestPermissions(), writer, store).ServeHTTP(response, auditMutationRequest(http.MethodDelete, deleteTestPath, ""))
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("delete response = %d %s", response.Code, response.Body.String())
			}
			if len(store.inputs) != 0 {
				t.Fatalf("failed delete created audit: %+v", store.inputs)
			}
			if strings.Contains(response.Body.String(), "secret SQL") || strings.Contains(response.Body.String(), "stack trace") {
				t.Fatalf("delete response leaked internals: %s", response.Body.String())
			}
		})
	}
}

func TestDeleteCommittedAuditFailureHasNoRetryOrCompensation(t *testing.T) {
	store := &testAuditStore{recordErr: errors.New("admin database unavailable")}
	deleteCalls := 0
	writer := recordWriterFunc{delete: func(context.Context, string, introspection.Resource, string) (map[string]any, error) {
		deleteCalls++
		return map[string]any{"id": "1", "name": "deleted"}, nil
	}}
	response := httptest.NewRecorder()
	newAuditTestRouter(testAuthService{user: testUser()}, adminTestPermissions(), writer, store).ServeHTTP(response, auditMutationRequest(http.MethodDelete, deleteTestPath, ""))

	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"AUDIT_WRITE_FAILED"`) || !strings.Contains(response.Body.String(), `"mutation_committed":true`) {
		t.Fatalf("audit failure response = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"record"`) {
		t.Fatalf("delete audit failure returned a saved record: %s", response.Body.String())
	}
	if deleteCalls != 1 || len(store.inputs) != 1 {
		t.Fatalf("delete calls = %d, audit calls = %d; automatic retry or compensation detected", deleteCalls, len(store.inputs))
	}
}

func TestDeleteRequiresSessionPermissionAndCSRF(t *testing.T) {
	deleteCalls := 0
	writer := recordWriterFunc{delete: func(context.Context, string, introspection.Resource, string) (map[string]any, error) {
		deleteCalls++
		return map[string]any{"id": "1"}, nil
	}}

	anonymous := httptest.NewRecorder()
	newAuditTestRouter(testAuthService{user: testUser()}, adminTestPermissions(), writer, nil).ServeHTTP(anonymous, httptest.NewRequest(http.MethodDelete, deleteTestPath, nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Errorf("anonymous status = %d", anonymous.Code)
	}

	withoutDelete := authorization.EffectivePermissions([]authorization.Grant{
		{Schema: "*", Resource: "*", Operation: authorization.Read},
		{Schema: "*", Resource: "*", Operation: authorization.Create},
		{Schema: "*", Resource: "*", Operation: authorization.Update},
	})
	for _, permissions := range []authorization.Permissions{withoutDelete, viewerTestPermissions()} {
		response := httptest.NewRecorder()
		newAuditTestRouter(testAuthService{user: testUser()}, permissions, writer, nil).ServeHTTP(response, auditMutationRequest(http.MethodDelete, deleteTestPath, ""))
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"FORBIDDEN"`) {
			t.Errorf("without delete status = %d, body = %s", response.Code, response.Body.String())
		}
	}

	withoutCSRF := httptest.NewRequest(http.MethodDelete, deleteTestPath, nil)
	withoutCSRF.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
	response := httptest.NewRecorder()
	newAuditTestRouter(testAuthService{user: testUser()}, adminTestPermissions(), writer, nil).ServeHTTP(response, withoutCSRF)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"CSRF_TOKEN_INVALID"`) {
		t.Errorf("without CSRF status = %d, body = %s", response.Code, response.Body.String())
	}
	if deleteCalls != 0 {
		t.Fatalf("unauthorized delete reached writer %d times", deleteCalls)
	}
}

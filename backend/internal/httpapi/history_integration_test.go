//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"velmir/backend/internal/audit"
	"velmir/backend/internal/auth"
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

type failingAuditStore struct {
	*audit.Store
}

func (failingAuditStore) Record(context.Context, audit.Input) error {
	return errors.New("local admin database is unavailable")
}

func TestLocalDatabasesRecordHistory(t *testing.T) {
	adminURL := os.Getenv("TEST_ADMIN_DATABASE_URL")
	databaseURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	if adminURL == "" || databaseURL == "" {
		t.Skip("TEST_ADMIN_DATABASE_URL and TEST_TARGET_DATABASE_URL are not set")
	}
	assertHistoryDatabaseURL(t, adminURL, "velmir_admin")
	assertHistoryDatabaseURL(t, databaseURL, "velmir_demo")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	adminTx, err := adminPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer adminTx.Rollback(ctx)
	readConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	readConfig.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	readPool, err := pgxpool.NewWithConfig(ctx, readConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer readPool.Close()
	writePool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer writePool.Close()

	authService, err := auth.NewService(auth.NewStore(adminTx), 15*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	provider := authorization.NewStore(adminTx)
	administration := auth.NewAdministration(adminTx)
	actor, err := auth.CreateUser(ctx, adminTx, "history-api-actor@example.test", "History Actor", "history-integration-password")
	if err != nil {
		t.Fatal(err)
	}
	var adminRoleID int64
	if err := adminTx.QueryRow(ctx, "SELECT id FROM roles WHERE name = 'admin'").Scan(&adminRoleID); err != nil {
		t.Fatal(err)
	}
	if err := administration.AssignRole(ctx, actor.ID, adminRoleID); err != nil {
		t.Fatal(err)
	}
	login, err := authService.Login(ctx, actor.Email, "history-integration-password", "127.0.0.1:1200")
	if err != nil {
		t.Fatal(err)
	}
	inspector := introspection.New(readPool)
	writer := records.NewWriter(writePool)
	lister := records.New(readPool)
	auditStore := audit.NewStore(adminTx)
	router := NewRouter(nil, readPool, inspector, lister, writer, authService, provider, auditStore, false)
	suffix := fmt.Sprintf("history-%d", time.Now().UnixNano())
	started := time.Now().UTC()

	call := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: login.Token})
		if method == http.MethodPost || method == http.MethodPatch || method == http.MethodDelete {
			request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
			request.Header.Set(csrfHeaderName, testCSRFToken)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	requireStatus := func(response *httptest.ResponseRecorder, wanted int) {
		t.Helper()
		if response.Code != wanted {
			t.Fatalf("status = %d, want %d; body = %s", response.Code, wanted, response.Body.String())
		}
	}
	recordFromResponse := func(response *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var body struct {
			Record map[string]any `json:"record"`
		}
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body.Record
	}
	historyPath := func(schema, resource string, key map[string]any, query string) string {
		t.Helper()
		encoded, err := json.Marshal(key)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("/api/resources/%s/%s/records/%s/history%s", schema, resource, url.PathEscape(string(encoded)), query)
	}

	createdResponse := call(http.MethodPost, "/api/resources/public/categories/records", `{"values":{"name":"`+suffix+`"}}`)
	requireStatus(createdResponse, http.StatusCreated)
	created := recordFromResponse(createdResponse)
	categoryID, ok := created["id"].(string)
	if !ok || created["name"] != suffix {
		t.Fatalf("created category = %#v", created)
	}
	updatedResponse := call(http.MethodPatch, fmt.Sprintf("/api/resources/public/categories/records/%s", url.PathEscape(fmt.Sprintf(`{"id":%q}`, categoryID))), `{"values":{"name":"`+suffix+`-updated"}}`)
	requireStatus(updatedResponse, http.StatusOK)
	updated := recordFromResponse(updatedResponse)
	if updated["name"] != suffix+"-updated" {
		t.Fatalf("updated category = %#v", updated)
	}

	key := map[string]any{"id": categoryID}
	firstPage := call(http.MethodGet, historyPath("public", "categories", key, "?page=1&limit=1"), "")
	requireStatus(firstPage, http.StatusOK)
	var history audit.HistoryResult
	if err := json.NewDecoder(firstPage.Body).Decode(&history); err != nil {
		t.Fatal(err)
	}
	if len(history.Events) != 1 || !history.HasNext || history.Events[0].Action != audit.Update {
		t.Fatalf("first history page = %+v", history)
	}
	event := history.Events[0]
	if event.Actor.ID != actor.ID || event.Actor.Email != actor.Email || event.Timestamp.Before(started.Add(-5*time.Second)) || event.Timestamp.After(time.Now().Add(5*time.Second)) {
		t.Errorf("actor/timestamp = %+v, started = %s", event, started)
	}
	if !bytes.Contains(event.Before, []byte(`"name":"`+suffix+`"`)) || !bytes.Contains(event.After, []byte(`"name":"`+suffix+`-updated"`)) || !bytes.Contains(event.RecordKey, []byte(`"id":"`+categoryID+`"`)) {
		t.Errorf("before/after/key = %s / %s / %s", event.Before, event.After, event.RecordKey)
	}
	secondPage := call(http.MethodGet, historyPath("public", "categories", key, "?page=2&limit=1"), "")
	requireStatus(secondPage, http.StatusOK)
	history = audit.HistoryResult{}
	if err := json.NewDecoder(secondPage.Body).Decode(&history); err != nil {
		t.Fatal(err)
	}
	if len(history.Events) != 1 || history.HasNext || history.Events[0].Action != audit.Create || string(history.Events[0].Before) != "null" {
		t.Fatalf("create history page = %+v", history)
	}

	beforeFailureCount := len(storeHistory(t, ctx, auditStore, key).Events)
	failed := call(http.MethodPost, "/api/resources/public/categories/records", `{"values":{}}`)
	requireStatus(failed, http.StatusBadRequest)
	if after := len(storeHistory(t, ctx, auditStore, key).Events); after != beforeFailureCount {
		t.Fatalf("failed mutation changed audit count from %d to %d", beforeFailureCount, after)
	}
	empty := call(http.MethodGet, historyPath("public", "categories", map[string]any{"id": "9223372036854775807"}, ""), "")
	requireStatus(empty, http.StatusOK)
	if !strings.Contains(empty.Body.String(), `"events":[]`) {
		t.Fatalf("empty history = %s", empty.Body.String())
	}

	anonymous := httptest.NewRecorder()
	router.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, historyPath("public", "categories", key, ""), nil))
	requireStatus(anonymous, http.StatusUnauthorized)
	noRole, err := auth.CreateUser(ctx, adminTx, "history-no-role@example.test", "No Role", "history-no-role-password")
	if err != nil {
		t.Fatal(err)
	}
	noRoleLogin, err := authService.Login(ctx, noRole.Email, "history-no-role-password", "127.0.0.1:1201")
	if err != nil {
		t.Fatal(err)
	}
	forbiddenRequest := httptest.NewRequest(http.MethodGet, historyPath("public", "categories", key, ""), nil)
	forbiddenRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: noRoleLogin.Token})
	forbidden := httptest.NewRecorder()
	router.ServeHTTP(forbidden, forbiddenRequest)
	requireStatus(forbidden, http.StatusForbidden)

	deletePath := fmt.Sprintf("/api/resources/public/categories/records/%s", url.PathEscape(fmt.Sprintf(`{"id":%q}`, categoryID)))
	anonymousDelete := httptest.NewRecorder()
	router.ServeHTTP(anonymousDelete, httptest.NewRequest(http.MethodDelete, deletePath, nil))
	requireStatus(anonymousDelete, http.StatusUnauthorized)
	withoutCSRF := httptest.NewRequest(http.MethodDelete, deletePath, nil)
	withoutCSRF.AddCookie(&http.Cookie{Name: sessionCookieName, Value: login.Token})
	withoutCSRFResponse := httptest.NewRecorder()
	router.ServeHTTP(withoutCSRFResponse, withoutCSRF)
	requireStatus(withoutCSRFResponse, http.StatusForbidden)
	viewer, err := auth.CreateUser(ctx, adminTx, "history-viewer@example.test", "Viewer", "history-viewer-password")
	if err != nil {
		t.Fatal(err)
	}
	var viewerRoleID int64
	if err := adminTx.QueryRow(ctx, "SELECT id FROM roles WHERE name = 'viewer'").Scan(&viewerRoleID); err != nil {
		t.Fatal(err)
	}
	if err := administration.AssignRole(ctx, viewer.ID, viewerRoleID); err != nil {
		t.Fatal(err)
	}
	viewerLogin, err := authService.Login(ctx, viewer.Email, "history-viewer-password", "127.0.0.1:1202")
	if err != nil {
		t.Fatal(err)
	}
	viewerDelete := httptest.NewRequest(http.MethodDelete, deletePath, nil)
	viewerDelete.AddCookie(&http.Cookie{Name: sessionCookieName, Value: viewerLogin.Token})
	viewerDelete.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	viewerDelete.Header.Set(csrfHeaderName, testCSRFToken)
	viewerResponse := httptest.NewRecorder()
	router.ServeHTTP(viewerResponse, viewerDelete)
	requireStatus(viewerResponse, http.StatusForbidden)

	authorResponse := call(http.MethodPost, "/api/resources/public/authors/records", `{"values":{"name":"`+suffix+`"}}`)
	requireStatus(authorResponse, http.StatusCreated)
	authorID := recordFromResponse(authorResponse)["id"].(string)
	bookResponse := call(http.MethodPost, "/api/resources/public/books/records", fmt.Sprintf(`{"values":{"author_id":%q,"title":%q,"metadata":{"big":9223372036854775807}}}`, authorID, suffix))
	requireStatus(bookResponse, http.StatusCreated)
	bookID := recordFromResponse(bookResponse)["id"].(string)
	compositeKey := map[string]any{"book_id": bookID, "category_id": categoryID}
	linkBody := fmt.Sprintf(`{"values":{"book_id":%q,"category_id":%q,"note":"before"}}`, bookID, categoryID)
	linkResponse := call(http.MethodPost, "/api/resources/public/book_categories/records", linkBody)
	requireStatus(linkResponse, http.StatusCreated)
	linkPath := fmt.Sprintf("/api/resources/public/book_categories/records/%s", url.PathEscape(fmt.Sprintf(`{"book_id":%q,"category_id":%q}`, bookID, categoryID)))
	requireStatus(call(http.MethodPatch, linkPath, `{"values":{"note":"after"}}`), http.StatusOK)
	compositeHistory := call(http.MethodGet, historyPath("public", "book_categories", compositeKey, "?limit=1"), "")
	requireStatus(compositeHistory, http.StatusOK)
	if !strings.Contains(compositeHistory.Body.String(), `"note":"before"`) || !strings.Contains(compositeHistory.Body.String(), `"note":"after"`) || !strings.Contains(compositeHistory.Body.String(), `"book_id":"`+bookID+`"`) {
		t.Fatalf("composite history = %s", compositeHistory.Body.String())
	}

	var auditCount int
	if err := adminTx.QueryRow(ctx, "SELECT count(*) FROM audit_events").Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	authorPath := fmt.Sprintf("/api/resources/public/authors/records/%s", url.PathEscape(fmt.Sprintf(`{"id":%q}`, authorID)))
	constraintFailure := call(http.MethodDelete, authorPath, "")
	requireStatus(constraintFailure, http.StatusBadRequest)
	if !strings.Contains(constraintFailure.Body.String(), `"code":"FOREIGN_KEY_VIOLATION"`) {
		t.Fatalf("constraint delete response = %s", constraintFailure.Body.String())
	}
	var afterConstraintCount int
	if err := adminTx.QueryRow(ctx, "SELECT count(*) FROM audit_events").Scan(&afterConstraintCount); err != nil || afterConstraintCount != auditCount {
		t.Fatalf("failed delete changed audit count from %d to %d, error = %v", auditCount, afterConstraintCount, err)
	}

	deletedLink := call(http.MethodDelete, linkPath, "")
	requireStatus(deletedLink, http.StatusNoContent)
	if deletedLink.Body.Len() != 0 {
		t.Fatalf("delete returned a body: %q", deletedLink.Body.String())
	}
	compositeHistory = call(http.MethodGet, historyPath("public", "book_categories", compositeKey, "?limit=1"), "")
	requireStatus(compositeHistory, http.StatusOK)
	if !strings.Contains(compositeHistory.Body.String(), `"action":"delete"`) || !strings.Contains(compositeHistory.Body.String(), `"note":"after"`) || !strings.Contains(compositeHistory.Body.String(), `"after":null`) {
		t.Fatalf("composite delete history = %s", compositeHistory.Body.String())
	}
	bookPath := fmt.Sprintf("/api/resources/public/books/records/%s", url.PathEscape(fmt.Sprintf(`{"id":%q}`, bookID)))
	requireStatus(call(http.MethodDelete, bookPath, ""), http.StatusNoContent)
	categoryPath := fmt.Sprintf("/api/resources/public/categories/records/%s", url.PathEscape(fmt.Sprintf(`{"id":%q}`, categoryID)))
	requireStatus(call(http.MethodDelete, categoryPath, ""), http.StatusNoContent)
	deletedHistory := call(http.MethodGet, historyPath("public", "categories", key, "?limit=1"), "")
	requireStatus(deletedHistory, http.StatusOK)
	if !strings.Contains(deletedHistory.Body.String(), `"action":"delete"`) || !strings.Contains(deletedHistory.Body.String(), `"name":"`+suffix+`-updated"`) || !strings.Contains(deletedHistory.Body.String(), `"after":null`) {
		t.Fatalf("deleted record history = %s", deletedHistory.Body.String())
	}
	beforeMissingCount := len(storeHistory(t, ctx, auditStore, key).Events)
	requireStatus(call(http.MethodDelete, categoryPath, ""), http.StatusNotFound)
	if after := len(storeHistory(t, ctx, auditStore, key).Events); after != beforeMissingCount {
		t.Fatalf("missing delete changed audit count from %d to %d", beforeMissingCount, after)
	}

	failingRouter := NewRouter(nil, readPool, inspector, lister, writer, authService, provider, failingAuditStore{Store: auditStore}, false)
	committedName := suffix + "-committed-without-audit"
	committedRequest := httptest.NewRequest(http.MethodPost, "/api/resources/public/categories/records", strings.NewReader(`{"values":{"name":"`+committedName+`"}}`))
	committedRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: login.Token})
	committedRequest.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	committedRequest.Header.Set(csrfHeaderName, testCSRFToken)
	committed := httptest.NewRecorder()
	failingRouter.ServeHTTP(committed, committedRequest)
	requireStatus(committed, http.StatusInternalServerError)
	if !strings.Contains(committed.Body.String(), `"code":"AUDIT_WRITE_FAILED"`) || !strings.Contains(committed.Body.String(), `"mutation_committed":true`) {
		t.Fatalf("committed audit failure = %s", committed.Body.String())
	}
	var storedName string
	if err := writePool.QueryRow(ctx, "SELECT name FROM public.categories WHERE name = $1", committedName).Scan(&storedName); err != nil || storedName != committedName {
		t.Fatalf("committed target record was compensated or lost: name = %q, error = %v", storedName, err)
	}

	auditFailureName := suffix + "-deleted-without-audit"
	auditFailureCreate := call(http.MethodPost, "/api/resources/public/categories/records", fmt.Sprintf(`{"values":{"name":%q}}`, auditFailureName))
	requireStatus(auditFailureCreate, http.StatusCreated)
	auditFailureID := recordFromResponse(auditFailureCreate)["id"].(string)
	auditFailureKey := map[string]any{"id": auditFailureID}
	auditFailurePath := fmt.Sprintf("/api/resources/public/categories/records/%s", url.PathEscape(fmt.Sprintf(`{"id":%q}`, auditFailureID)))
	auditFailureRequest := httptest.NewRequest(http.MethodDelete, auditFailurePath, nil)
	auditFailureRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: login.Token})
	auditFailureRequest.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	auditFailureRequest.Header.Set(csrfHeaderName, testCSRFToken)
	auditFailureResponse := httptest.NewRecorder()
	failingRouter.ServeHTTP(auditFailureResponse, auditFailureRequest)
	requireStatus(auditFailureResponse, http.StatusInternalServerError)
	if !strings.Contains(auditFailureResponse.Body.String(), `"code":"AUDIT_WRITE_FAILED"`) || !strings.Contains(auditFailureResponse.Body.String(), `"mutation_committed":true`) {
		t.Fatalf("committed delete audit failure = %s", auditFailureResponse.Body.String())
	}
	var stillExists bool
	if err := writePool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM public.categories WHERE id = $1)", auditFailureID).Scan(&stillExists); err != nil || stillExists {
		t.Fatalf("deleted record was restored or compensated: exists = %v, error = %v", stillExists, err)
	}
	if events := storeHistory(t, ctx, auditStore, auditFailureKey).Events; len(events) != 1 || events[0].Action != audit.Create {
		t.Fatalf("failed audit was retried or wrote a delete event: %+v", events)
	}
}

func storeHistory(t *testing.T, ctx context.Context, store *audit.Store, key map[string]any) audit.HistoryResult {
	t.Helper()
	encoded, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.History(ctx, "public", "categories", encoded, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertHistoryDatabaseURL(t *testing.T, raw, database string) {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Path != "/"+database || !localIntegrationHost(parsed.Hostname()) {
		t.Fatalf("integration database URL must be local and use %s", database)
	}
}

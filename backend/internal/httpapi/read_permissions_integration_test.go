//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
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

func TestReadPermissionsLocalAdminAndDemoAPI(t *testing.T) {
	adminURL, targetURL := os.Getenv("TEST_ADMIN_DATABASE_URL"), os.Getenv("TEST_TARGET_DATABASE_URL")
	if adminURL == "" || targetURL == "" {
		t.Skip("local admin and demo database URLs are required")
	}
	assertHistoryDatabaseURL(t, adminURL, "velmir_admin")
	assertHistoryDatabaseURL(t, targetURL, "velmir_demo")
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
	readConfig, err := pgxpool.ParseConfig(targetURL)
	if err != nil {
		t.Fatal(err)
	}
	readConfig.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	readPool, err := pgxpool.NewWithConfig(ctx, readConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer readPool.Close()
	writePool, err := pgxpool.New(ctx, targetURL)
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
	suffix := fmt.Sprintf("read-%d", time.Now().UnixNano())
	actor, err := auth.CreateUser(ctx, adminTx, suffix+"@example.test", "Read actor", "read-integration-password")
	if err != nil {
		t.Fatal(err)
	}
	rolePermissions := func(readColumns []string, readMode authorization.FieldPermissionMode) authorization.Permissions {
		return authorization.Permissions{{
			Schema: "public", Resource: "books", Operations: []authorization.Operation{authorization.Read, authorization.Create, authorization.Update},
			FieldPermissions: []authorization.FieldPermission{
				{Action: authorization.Read, Mode: readMode, Columns: readColumns},
				{Action: authorization.Create, Mode: authorization.FieldAllowlist, Columns: []string{"author_id", "title", "isbn"}},
				{Action: authorization.Update, Mode: authorization.FieldAllowlist, Columns: []string{"isbn"}},
			},
		}}
	}
	role1, err := administration.CreateRole(ctx, suffix+"-title", "", rolePermissions([]string{"title"}, authorization.FieldAllowlist))
	if err != nil {
		t.Fatal(err)
	}
	if err := administration.AssignRole(ctx, actor.ID, role1.ID); err != nil {
		t.Fatal(err)
	}
	login, err := authService.Login(ctx, actor.Email, "read-integration-password", "127.0.0.1:1400")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(nil, readPool, introspection.New(readPool), records.New(readPool), records.NewWriter(writePool), authService, provider, audit.NewStore(adminTx), false)
	call := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: login.Token})
		if method != http.MethodGet {
			request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
			request.Header.Set(csrfHeaderName, testCSRFToken)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	requireStatus := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("status = %d, want %d; body = %s", response.Code, want, response.Body.String())
		}
	}
	var authorID string
	if err := readPool.QueryRow(ctx, "SELECT id::text FROM public.authors ORDER BY id LIMIT 1").Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	created := call(http.MethodPost, "/api/resources/public/books/records", `{"values":{"author_id":"`+authorID+`","title":"`+suffix+`","isbn":"8b-hidden"}}`)
	requireStatus(created, http.StatusCreated)
	if strings.Contains(created.Body.String(), "8b-hidden") || strings.Contains(created.Body.String(), "author_id") || !strings.Contains(created.Body.String(), `"title":"`+suffix+`"`) {
		t.Fatalf("create response disclosed write-only fields: %s", created.Body.String())
	}
	var createdBody struct {
		Record map[string]any `json:"record"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdBody); err != nil {
		t.Fatal(err)
	}
	bookID := createdBody.Record["id"].(string)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = writePool.Exec(cleanupCtx, "DELETE FROM public.books WHERE id = $1", bookID)
	}()
	key := url.PathEscape(fmt.Sprintf(`{"id":%q}`, bookID))
	updated := call(http.MethodPatch, "/api/resources/public/books/records/"+key, `{"values":{"isbn":"updated-8b-hidden"}}`)
	requireStatus(updated, http.StatusOK)
	if strings.Contains(updated.Body.String(), "updated-8b-hidden") || strings.Contains(updated.Body.String(), `"isbn"`) {
		t.Fatalf("update response disclosed write-only field: %s", updated.Body.String())
	}
	list := call(http.MethodGet, "/api/resources/public/books/records?filter="+url.QueryEscape(`[{"column":"title","operator":"eq","value":"`+suffix+`"}]`), "")
	requireStatus(list, http.StatusOK)
	if strings.Contains(list.Body.String(), "write-only") || strings.Contains(list.Body.String(), "author_id") || !strings.Contains(list.Body.String(), suffix) {
		t.Fatalf("restricted list = %s", list.Body.String())
	}
	auditBefore := 0
	if err := adminTx.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE actor_user_id = $1", actor.ID).Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"/api/resources/public/books/records?sort=isbn",
		"/api/resources/public/books/records?search=hidden&search_column=isbn",
		"/api/resources/public/books/records?filter=" + url.QueryEscape(`[{"column":"isbn","operator":"eq","value":"hidden"}]`),
	} {
		response := call(http.MethodGet, target, "")
		requireStatus(response, http.StatusForbidden)
		if !strings.Contains(response.Body.String(), "FIELD_NOT_READABLE") {
			t.Fatalf("forbidden read = %s", response.Body.String())
		}
	}
	auditAfter := 0
	if err := adminTx.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE actor_user_id = $1", actor.ID).Scan(&auditAfter); err != nil {
		t.Fatal(err)
	}
	if auditAfter != auditBefore {
		t.Fatalf("forbidden reads changed audit: %d -> %d", auditBefore, auditAfter)
	}

	historyPath := "/api/resources/public/books/records/" + key + "/history"
	history := call(http.MethodGet, historyPath, "")
	requireStatus(history, http.StatusOK)
	if strings.Contains(history.Body.String(), "write-only") || strings.Contains(history.Body.String(), "isbn") || strings.Contains(history.Body.String(), "author_id") {
		t.Fatalf("history disclosed hidden values: %s", history.Body.String())
	}
	resolve := call(http.MethodPost, "/api/resources/public/books/relations/resolve", `{"relations":[{"name":"books_author_id_fkey","keys":[{"id":"`+authorID+`"}]}]}`)
	requireStatus(resolve, http.StatusForbidden)

	role2, err := administration.CreateRole(ctx, suffix+"-isbn", "", authorization.Permissions{
		{Schema: "public", Resource: "books", Operations: []authorization.Operation{authorization.Read}, FieldPermissions: []authorization.FieldPermission{{Action: authorization.Read, Mode: authorization.FieldAllowlist, Columns: []string{"isbn"}}}},
		{Schema: "public", Resource: "authors", Operations: []authorization.Operation{authorization.Read}, FieldPermissions: []authorization.FieldPermission{{Action: authorization.Read, Mode: authorization.FieldAllowlist, Columns: []string{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := administration.AssignRole(ctx, actor.ID, role2.ID); err != nil {
		t.Fatal(err)
	}
	history = call(http.MethodGet, historyPath, "")
	requireStatus(history, http.StatusOK)
	if !strings.Contains(history.Body.String(), "updated-8b-hidden") || strings.Contains(history.Body.String(), "author_id") {
		t.Fatalf("role union did not apply to old history: %s", history.Body.String())
	}
	schema := call(http.MethodGet, "/api/schema", "")
	requireStatus(schema, http.StatusOK)
	var catalog introspection.Catalog
	if err := json.NewDecoder(schema.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	books, _ := catalog.FindResource("public", "books")
	authors, _ := catalog.FindResource("public", "authors")
	if len(books.ReadableFields) != 3 || len(authors.ReadableFields) != 1 || authors.ReadableFields[0] != "id" || len(authors.ReverseRelations) != 0 {
		t.Fatalf("union/PK-only schema = books %+v, authors %+v", books.ReadableFields, authors.ReadableFields)
	}

	role3, err := administration.CreateRole(ctx, suffix+"-unrestricted", "", authorization.EffectivePermissions([]authorization.Grant{{Schema: "public", Resource: "books", Operation: authorization.Read}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := administration.AssignRole(ctx, actor.ID, role3.ID); err != nil {
		t.Fatal(err)
	}
	history = call(http.MethodGet, historyPath, "")
	requireStatus(history, http.StatusOK)
	if !strings.Contains(history.Body.String(), "author_id") || !strings.Contains(history.Body.String(), "8b-hidden") {
		t.Fatalf("unrestricted + restricted did not restore full history: %s", history.Body.String())
	}
}

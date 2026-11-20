//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestLocalDatabasesRelationResolveAndReverseMetadata(t *testing.T) {
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
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	adminUser, err := auth.CreateUser(ctx, adminTx, "relation-admin-"+suffix+"@example.test", "Relation Admin", "relation-integration-password")
	if err != nil {
		t.Fatal(err)
	}
	var adminRoleID int64
	if err := adminTx.QueryRow(ctx, "SELECT id FROM roles WHERE name = 'admin'").Scan(&adminRoleID); err != nil {
		t.Fatal(err)
	}
	if err := administration.AssignRole(ctx, adminUser.ID, adminRoleID); err != nil {
		t.Fatal(err)
	}
	booksUser, err := auth.CreateUser(ctx, adminTx, "relation-books-"+suffix+"@example.test", "Books Only", "relation-integration-password")
	if err != nil {
		t.Fatal(err)
	}
	booksRole, err := administration.CreateRole(ctx, "relation-books-"+suffix, "", authorization.EffectivePermissions([]authorization.Grant{{Schema: "public", Resource: "books", Operation: authorization.Read}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := administration.AssignRole(ctx, booksUser.ID, booksRole.ID); err != nil {
		t.Fatal(err)
	}
	authorsUser, err := auth.CreateUser(ctx, adminTx, "relation-authors-"+suffix+"@example.test", "Authors Only", "relation-integration-password")
	if err != nil {
		t.Fatal(err)
	}
	authorsRole, err := administration.CreateRole(ctx, "relation-authors-"+suffix, "", authorization.EffectivePermissions([]authorization.Grant{{Schema: "public", Resource: "authors", Operation: authorization.Read}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := administration.AssignRole(ctx, authorsUser.ID, authorsRole.ID); err != nil {
		t.Fatal(err)
	}
	adminLogin, err := authService.Login(ctx, adminUser.Email, "relation-integration-password", "127.0.0.1:1300")
	if err != nil {
		t.Fatal(err)
	}
	booksLogin, err := authService.Login(ctx, booksUser.Email, "relation-integration-password", "127.0.0.1:1301")
	if err != nil {
		t.Fatal(err)
	}
	authorsLogin, err := authService.Login(ctx, authorsUser.Email, "relation-integration-password", "127.0.0.1:1302")
	if err != nil {
		t.Fatal(err)
	}

	inspector := introspection.New(readPool)
	lister := records.New(readPool)
	writer := records.NewWriter(writePool)
	auditStore := audit.NewStore(adminTx)
	router := NewRouter(nil, readPool, inspector, lister, writer, authService, provider, auditStore, false)
	call := func(token, method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		if token != "" {
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	var auditBefore int
	if err := adminTx.QueryRow(ctx, "SELECT count(*) FROM audit_events").Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}

	resolved := call(adminLogin.Token, http.MethodPost, "/api/resources/public/books/relations/resolve", `{"relations":[{"name":"books_author_id_fkey","keys":[{"id":"1"},{"id":1},{"id":null}]}]}`)
	if resolved.Code != http.StatusOK {
		t.Fatalf("batch status = %d, body = %s", resolved.Code, resolved.Body.String())
	}
	var result records.RelationResolveResult
	if err := json.NewDecoder(resolved.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Relations) != 1 || len(result.Relations[0].Records) != 1 || result.Relations[0].Records[0]["id"] != "1" {
		t.Fatalf("batch result = %+v", result.Relations)
	}
	empty := call(adminLogin.Token, http.MethodPost, "/api/resources/public/books/relations/resolve", `{"relations":[{"name":"books_author_id_fkey","keys":[{"id":null}]}]}`)
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"records":[]`) {
		t.Fatalf("null-key batch = %d %s", empty.Code, empty.Body.String())
	}

	schemaResponse := call(adminLogin.Token, http.MethodGet, "/api/schema", "")
	var catalog introspection.Catalog
	if err := json.NewDecoder(schemaResponse.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	authors, ok := catalog.FindResource("public", "authors")
	if !ok || len(authors.ReverseRelations) == 0 {
		t.Fatalf("authors reverse metadata = %+v", authors.ReverseRelations)
	}
	foundBooks := false
	for _, relation := range authors.ReverseRelations {
		if relation.Resource == "books" && relation.Name == "books_author_id_fkey" && len(relation.Columns) == 1 && relation.Columns[0] == "author_id" && relation.ReferencedColumns[0] == "id" {
			foundBooks = true
		}
	}
	if !foundBooks {
		t.Errorf("books reverse relation is missing: %+v", authors.ReverseRelations)
	}

	restrictedSchema := call(booksLogin.Token, http.MethodGet, "/api/schema", "")
	if restrictedSchema.Code != http.StatusOK || strings.Contains(restrictedSchema.Body.String(), `"name":"authors"`) || strings.Contains(restrictedSchema.Body.String(), "books_author_id_fkey") {
		t.Fatalf("restricted schema leaks authors = %d %s", restrictedSchema.Code, restrictedSchema.Body.String())
	}
	forbiddenReferenced := call(booksLogin.Token, http.MethodPost, "/api/resources/public/books/relations/resolve", `{"relations":[{"name":"books_author_id_fkey","keys":[]}]}`)
	if forbiddenReferenced.Code != http.StatusForbidden || strings.Contains(forbiddenReferenced.Body.String(), "authors") {
		t.Fatalf("referenced read check = %d %s", forbiddenReferenced.Code, forbiddenReferenced.Body.String())
	}
	forbiddenCurrent := call(authorsLogin.Token, http.MethodPost, "/api/resources/public/books/relations/resolve", `{"relations":[]}`)
	if forbiddenCurrent.Code != http.StatusForbidden {
		t.Fatalf("current read check = %d %s", forbiddenCurrent.Code, forbiddenCurrent.Body.String())
	}
	anonymous := call("", http.MethodPost, "/api/resources/public/books/relations/resolve", `{"relations":[]}`)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", anonymous.Code)
	}
	unknown := call(adminLogin.Token, http.MethodPost, "/api/resources/public/books/relations/resolve", `{"relations":[{"name":"unknown; DROP TABLE books","keys":[]}]}`)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown relation status = %d %s", unknown.Code, unknown.Body.String())
	}

	var auditAfter int
	if err := adminTx.QueryRow(ctx, "SELECT count(*) FROM audit_events").Scan(&auditAfter); err != nil {
		t.Fatal(err)
	}
	if auditAfter != auditBefore {
		t.Fatalf("read-only resolution changed audit count from %d to %d", auditBefore, auditAfter)
	}
}

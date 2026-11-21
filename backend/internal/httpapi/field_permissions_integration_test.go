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

func TestFieldPermissionsLocalAdminAndDemoAPI(t *testing.T) {
	adminURL := os.Getenv("TEST_ADMIN_DATABASE_URL")
	targetURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	if adminURL == "" || targetURL == "" {
		t.Skip("local admin and demo database URLs are required")
	}
	adminParsed, adminErr := url.Parse(adminURL)
	targetParsed, targetErr := url.Parse(targetURL)
	if adminErr != nil || targetErr != nil || adminParsed.Path != "/velmir_admin" || targetParsed.Path != "/velmir_demo" ||
		!localIntegrationHost(adminParsed.Hostname()) || !localIntegrationHost(targetParsed.Hostname()) {
		t.Fatal("field permission integration tests require only local velmir_admin and velmir_demo databases")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	writer := records.NewWriter(writePool)
	inspector := introspection.New(readPool)
	auditStore := audit.NewStore(adminTx)
	suffix := fmt.Sprintf("field-api-%d", time.Now().UnixNano())
	actor, err := auth.CreateUser(ctx, adminTx, suffix+"@example.test", "Field actor", "field-integration-password")
	if err != nil {
		t.Fatal(err)
	}
	rolePermissions := func(createColumns, updateColumns []string, createMode, updateMode authorization.FieldPermissionMode) authorization.Permissions {
		return authorization.Permissions{{
			Schema: "public", Resource: "books", Operations: []authorization.Operation{authorization.Read, authorization.Create, authorization.Update},
			FieldPermissions: []authorization.FieldPermission{
				{Action: authorization.Create, Mode: createMode, Columns: createColumns},
				{Action: authorization.Update, Mode: updateMode, Columns: updateColumns},
			},
		}}
	}
	role1, err := administration.CreateRole(ctx, suffix+"_one", "", rolePermissions([]string{"author_id", "title"}, []string{"title"}, authorization.FieldAllowlist, authorization.FieldAllowlist))
	if err != nil {
		t.Fatal(err)
	}
	role2, err := administration.CreateRole(ctx, suffix+"_two", "", rolePermissions([]string{}, []string{"published_on"}, authorization.FieldAllowlist, authorization.FieldAllowlist))
	if err != nil {
		t.Fatal(err)
	}
	role3, err := administration.CreateRole(ctx, suffix+"_three", "", rolePermissions([]string{}, []string{}, authorization.FieldAllowlist, authorization.FieldUnrestricted))
	if err != nil {
		t.Fatal(err)
	}
	for _, roleID := range []int64{role1.ID} {
		if err := administration.AssignRole(ctx, actor.ID, roleID); err != nil {
			t.Fatal(err)
		}
	}
	login, err := authService.Login(ctx, actor.Email, "field-integration-password", "127.0.0.1:1100")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(nil, readPool, inspector, records.New(readPool), writer, authService, provider, auditStore, false)
	request := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: login.Token})
		if method != http.MethodGet {
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
			req.Header.Set(csrfHeaderName, testCSRFToken)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	requireStatus := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("status = %d, want %d; body = %s", response.Code, want, response.Body.String())
		}
	}
	bookKey := func(id string) string {
		return url.PathEscape(fmt.Sprintf(`{"id":%q}`, id))
	}
	schemaResource := func() introspection.Resource {
		t.Helper()
		response := request(http.MethodGet, "/api/schema", "")
		requireStatus(response, http.StatusOK)
		var catalog introspection.Catalog
		if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
			t.Fatal(err)
		}
		resource, ok := catalog.FindResource("public", "books")
		if !ok {
			t.Fatal("books metadata is unavailable")
		}
		return resource
	}
	auditCount := func() int {
		t.Helper()
		var count int
		if err := adminTx.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE actor_user_id = $1", actor.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	catalog, err := inspector.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	books, _ := catalog.FindResource("public", "books")
	var authorID string
	if err := readPool.QueryRow(ctx, "SELECT id::text FROM public.authors ORDER BY id LIMIT 1").Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	createdIDs := make([]string, 0)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, id := range createdIDs {
			_, _ = writePool.Exec(cleanupCtx, "DELETE FROM public.books WHERE id = $1", id)
		}
	}()
	initial, err := writer.Create(ctx, "public", books, testValues(t, map[string]any{"author_id": authorID, "title": suffix}))
	if err != nil {
		t.Fatal(err)
	}
	initialID := initial["id"].(string)
	createdIDs = append(createdIDs, initialID)

	resource := schemaResource()
	if len(resource.UpdatableFields) != 1 || resource.UpdatableFields[0] != "title" || !resource.CanCreate || !resource.CanUpdate {
		t.Fatalf("initial effective fields = %+v", resource)
	}
	requireStatus(request(http.MethodPatch, "/api/resources/public/books/records/"+bookKey(initialID), `{"values":{"title":"`+suffix+`-allowed"}}`), http.StatusOK)
	if auditCount() != 1 {
		t.Fatalf("allowed update audit count = %d", auditCount())
	}
	for _, body := range []string{`{"values":{"isbn":"forbidden"}}`, `{"values":{"title":"` + suffix + `-mixed","isbn":"forbidden"}}`} {
		response := request(http.MethodPatch, "/api/resources/public/books/records/"+bookKey(initialID), body)
		requireStatus(response, http.StatusForbidden)
		if !strings.Contains(response.Body.String(), "FIELD_FORBIDDEN") {
			t.Fatalf("forbidden response = %s", response.Body.String())
		}
	}
	var currentTitle string
	if err := readPool.QueryRow(ctx, "SELECT title FROM public.books WHERE id = $1", initialID).Scan(&currentTitle); err != nil {
		t.Fatal(err)
	}
	if currentTitle != suffix+"-allowed" || auditCount() != 1 {
		t.Fatalf("forbidden/mixed update changed title to %q or audit to %d", currentTitle, auditCount())
	}

	if err := administration.AssignRole(ctx, actor.ID, role2.ID); err != nil {
		t.Fatal(err)
	}
	resource = schemaResource()
	if len(resource.UpdatableFields) != 2 {
		t.Fatalf("two-role union = %+v", resource.UpdatableFields)
	}
	requireStatus(request(http.MethodPatch, "/api/resources/public/books/records/"+bookKey(initialID), `{"values":{"published_on":"2026-01-02"}}`), http.StatusOK)

	if err := administration.AssignRole(ctx, actor.ID, role3.ID); err != nil {
		t.Fatal(err)
	}
	resource = schemaResource()
	if len(resource.UpdatableFields) < 5 {
		t.Fatalf("unrestricted plus restricted did not dominate: %+v", resource.UpdatableFields)
	}
	requireStatus(request(http.MethodPatch, "/api/resources/public/books/records/"+bookKey(initialID), `{"values":{"isbn":"8a-isbn"}}`), http.StatusOK)

	createResponse := request(http.MethodPost, "/api/resources/public/books/records", `{"values":{"author_id":"`+authorID+`","title":"`+suffix+`-created"}}`)
	requireStatus(createResponse, http.StatusCreated)
	var createdBody struct {
		Record map[string]any `json:"record"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&createdBody); err != nil {
		t.Fatal(err)
	}
	createdID := createdBody.Record["id"].(string)
	createdIDs = append(createdIDs, createdID)
	if createdBody.Record["metadata"] == nil {
		t.Fatal("database default metadata was not applied")
	}

	if err := administration.ReplaceRolePermissions(ctx, role1.ID, rolePermissions([]string{}, []string{}, authorization.FieldAllowlist, authorization.FieldAllowlist)); err != nil {
		t.Fatal(err)
	}
	resource = schemaResource()
	if resource.CanCreate || resource.CreateDisabledReason != "required_field_unavailable" {
		t.Fatalf("empty create allowlist metadata = %+v", resource)
	}
	requireStatus(request(http.MethodPost, "/api/resources/public/books/records", `{"values":{}}`), http.StatusForbidden)
	if err := administration.RemoveRole(ctx, actor.ID, actor.ID, role2.ID); err != nil {
		t.Fatal(err)
	}
	if err := administration.RemoveRole(ctx, actor.ID, actor.ID, role3.ID); err != nil {
		t.Fatal(err)
	}
	resource = schemaResource()
	if resource.CanUpdate || resource.UpdateDisabledReason != "no_writable_fields" {
		t.Fatalf("empty update allowlist metadata = %+v", resource)
	}
	response := request(http.MethodPatch, "/api/resources/public/books/records/"+bookKey(initialID), `{"values":{}}`)
	requireStatus(response, http.StatusForbidden)
	if !strings.Contains(response.Body.String(), "NO_WRITABLE_FIELDS") {
		t.Fatalf("empty update response = %s", response.Body.String())
	}
}

func testValues(t *testing.T, values map[string]any) records.RecordValues {
	t.Helper()
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var result records.RecordValues
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

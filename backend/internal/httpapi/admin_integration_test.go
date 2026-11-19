//go:build integration

package httpapi

import (
	"context"
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

	"velmir/backend/internal/auth"
	"velmir/backend/internal/authorization"
)

func TestLocalAdminDatabaseAdministrationAPI(t *testing.T) {
	databaseURL := os.Getenv("TEST_ADMIN_DATABASE_URL")
	parsed, err := url.Parse(databaseURL)
	if databaseURL == "" {
		t.Skip("TEST_ADMIN_DATABASE_URL is not set")
	}
	if err != nil || parsed.Path != "/velmir_admin" || !localIntegrationHost(parsed.Hostname()) {
		t.Fatal("TEST_ADMIN_DATABASE_URL must point to the local velmir_admin database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	authService, err := auth.NewService(auth.NewStore(tx), 15*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	provider := authorization.NewStore(tx)
	administration := auth.NewAdministration(tx)
	router := NewRouter(nil, nil, nil, nil, nil, authService, provider, false, administration)

	actorPassword := "integration-actor-password"
	actor, err := auth.CreateUser(ctx, tx, "admin-api-actor@example.test", "Actor", actorPassword)
	if err != nil {
		t.Fatal(err)
	}
	var adminRoleID int64
	if err := tx.QueryRow(ctx, "SELECT id FROM roles WHERE name = 'admin'").Scan(&adminRoleID); err != nil {
		t.Fatal(err)
	}
	if err := administration.AssignRole(ctx, actor.ID, adminRoleID); err != nil {
		t.Fatal(err)
	}
	login, err := authService.Login(ctx, actor.Email, actorPassword, "127.0.0.1:1000")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, target, body string, withCSRF bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: login.Token})
		if withCSRF {
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

	requireStatus(request(http.MethodGet, "/api/auth/me", "", false), http.StatusOK)
	response := request(http.MethodPost, "/api/admin/users", `{"email":"managed@example.test","display_name":"Managed","password":"managed-initial-password"}`, true)
	requireStatus(response, http.StatusCreated)
	var managedID int64
	if err := tx.QueryRow(ctx, "SELECT id FROM users WHERE email = 'managed@example.test'").Scan(&managedID); err != nil {
		t.Fatal(err)
	}
	requireStatus(request(http.MethodPatch, fmt.Sprintf("/api/admin/users/%d/display-name", managedID), `{"display_name":"Managed user"}`, true), http.StatusNoContent)
	requireStatus(request(http.MethodGet, "/api/admin/users", "", false), http.StatusOK)

	viewerLogin, err := authService.Login(ctx, "managed@example.test", "managed-initial-password", "127.0.0.1:1001")
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(request(http.MethodPatch, fmt.Sprintf("/api/admin/users/%d/status", managedID), `{"is_active":false}`, true), http.StatusNoContent)
	if _, err := authService.Authenticate(ctx, viewerLogin.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("disabled user's session error = %v", err)
	}
	if _, err := authService.Login(ctx, "managed@example.test", "managed-initial-password", "127.0.0.1:1002"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("disabled user's login error = %v", err)
	}
	requireStatus(request(http.MethodPatch, fmt.Sprintf("/api/admin/users/%d/status", managedID), `{"is_active":true}`, true), http.StatusNoContent)
	viewerLogin, err = authService.Login(ctx, "managed@example.test", "managed-initial-password", "127.0.0.1:1003")
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(request(http.MethodPost, fmt.Sprintf("/api/admin/users/%d/password-reset", managedID), `{"password":"managed-replacement-password"}`, true), http.StatusNoContent)
	if _, err := authService.Authenticate(ctx, viewerLogin.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("password reset did not revoke session: %v", err)
	}
	if _, err := authService.Login(ctx, "managed@example.test", "managed-initial-password", "127.0.0.1:1004"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("old password still works: %v", err)
	}

	response = request(http.MethodPost, "/api/admin/roles", `{"name":"integration_editor","description":"Integration editor","permissions":[{"schema":"public","resource":"books","operations":["read"]}]}`, true)
	requireStatus(response, http.StatusCreated)
	var editorRoleID int64
	if err := tx.QueryRow(ctx, "SELECT id FROM roles WHERE name = 'integration_editor'").Scan(&editorRoleID); err != nil {
		t.Fatal(err)
	}
	requireStatus(request(http.MethodPatch, fmt.Sprintf("/api/admin/roles/%d", editorRoleID), `{"name":"integration_editor","description":"Updated editor"}`, true), http.StatusNoContent)
	requireStatus(request(http.MethodPut, fmt.Sprintf("/api/admin/users/%d/roles/%d", managedID, editorRoleID), "", true), http.StatusNoContent)
	permissions, err := provider.Permissions(ctx, managedID)
	if err != nil || !permissions.Allows("public", "books", authorization.Read) {
		t.Fatalf("assigned permissions = %#v, error = %v", permissions, err)
	}
	requireStatus(request(http.MethodPut, fmt.Sprintf("/api/admin/roles/%d/permissions", editorRoleID), `{"permissions":[{"schema":"sales","resource":"orders","operations":["update"]}]}`, true), http.StatusNoContent)
	permissions, err = provider.Permissions(ctx, managedID)
	if err != nil || permissions.Allows("public", "books", authorization.Read) || !permissions.Allows("sales", "orders", authorization.Update) {
		t.Fatalf("permissions were not immediately replaced: %#v, error = %v", permissions, err)
	}
	response = request(http.MethodPost, "/api/admin/roles", `{"name":"integration_reader","description":"Reader","permissions":[{"schema":"archive","resource":"items","operations":["read"]}]}`, true)
	requireStatus(response, http.StatusCreated)
	var readerRoleID int64
	if err := tx.QueryRow(ctx, "SELECT id FROM roles WHERE name = 'integration_reader'").Scan(&readerRoleID); err != nil {
		t.Fatal(err)
	}
	requireStatus(request(http.MethodPut, fmt.Sprintf("/api/admin/users/%d/roles/%d", managedID, readerRoleID), "", true), http.StatusNoContent)
	permissions, err = provider.Permissions(ctx, managedID)
	if err != nil || !permissions.Allows("sales", "orders", authorization.Update) || !permissions.Allows("archive", "items", authorization.Read) {
		t.Fatalf("roles were not unioned: %#v, error = %v", permissions, err)
	}
	requireStatus(request(http.MethodGet, "/api/admin/roles", "", false), http.StatusOK)
	requireStatus(request(http.MethodDelete, fmt.Sprintf("/api/admin/users/%d/roles/%d", managedID, readerRoleID), "", true), http.StatusNoContent)

	var viewerRoleID int64
	if err := tx.QueryRow(ctx, "SELECT id FROM roles WHERE name = 'viewer'").Scan(&viewerRoleID); err != nil {
		t.Fatal(err)
	}
	requireStatus(request(http.MethodPatch, fmt.Sprintf("/api/admin/roles/%d", viewerRoleID), `{"name":"renamed","description":""}`, true), http.StatusConflict)
	requireStatus(request(http.MethodPut, fmt.Sprintf("/api/admin/roles/%d/permissions", viewerRoleID), `{"permissions":[]}`, true), http.StatusConflict)
	requireStatus(request(http.MethodPatch, fmt.Sprintf("/api/admin/users/%d/status", actor.ID), `{"is_active":false}`, true), http.StatusConflict)
	requireStatus(request(http.MethodDelete, fmt.Sprintf("/api/admin/users/%d/roles/%d", actor.ID, adminRoleID), "", true), http.StatusConflict)

	secondAdmin, err := auth.CreateUser(ctx, tx, "second-admin@example.test", "Second admin", "second-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(request(http.MethodPut, fmt.Sprintf("/api/admin/users/%d/roles/%d", secondAdmin.ID, adminRoleID), "", true), http.StatusNoContent)
	requireStatus(request(http.MethodPatch, fmt.Sprintf("/api/admin/users/%d/status", secondAdmin.ID), `{"is_active":false}`, true), http.StatusNoContent)
	requireStatus(request(http.MethodPatch, fmt.Sprintf("/api/admin/users/%d/status", actor.ID), `{"is_active":false}`, true), http.StatusConflict)
	if _, err := tx.Exec(ctx, `
		UPDATE users SET is_active = FALSE
		WHERE id <> $1 AND is_active AND EXISTS (
			SELECT 1 FROM user_roles AS ur
			JOIN roles AS r ON r.id = ur.role_id
			WHERE ur.user_id = users.id AND r.name = 'admin' AND r.is_system
		)`, actor.ID); err != nil {
		t.Fatal(err)
	}
	if err := administration.SetUserActive(ctx, secondAdmin.ID, actor.ID, false); !errors.Is(err, auth.ErrLastActiveAdmin) {
		t.Fatalf("last active admin disable error = %v", err)
	}
	if err := administration.RemoveRole(ctx, secondAdmin.ID, actor.ID, adminRoleID); !errors.Is(err, auth.ErrLastActiveAdmin) {
		t.Fatalf("last active admin role removal error = %v", err)
	}
}

func localIntegrationHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

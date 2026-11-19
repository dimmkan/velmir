package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"velmir/backend/internal/auth"
	"velmir/backend/internal/authorization"
)

type fakeAdministrationService struct {
	users   []auth.UserSummary
	roles   []auth.Role
	err     error
	calls   []string
	actorID int64
	userID  int64
	roleID  int64
}

func (s *fakeAdministrationService) ListUsers(context.Context) ([]auth.UserSummary, error) {
	s.calls = append(s.calls, "list users")
	return s.users, s.err
}
func (s *fakeAdministrationService) CreateUser(context.Context, string, string, string) (auth.UserSummary, error) {
	s.calls = append(s.calls, "create user")
	return auth.UserSummary{ID: 2, Email: "new@example.test", DisplayName: "New user", IsActive: true, Roles: []string{}}, s.err
}
func (s *fakeAdministrationService) UpdateDisplayName(context.Context, int64, string) error {
	s.calls = append(s.calls, "display name")
	return s.err
}
func (s *fakeAdministrationService) SetUserActive(_ context.Context, actorID, userID int64, _ bool) error {
	s.calls = append(s.calls, "status")
	s.actorID, s.userID = actorID, userID
	return s.err
}
func (s *fakeAdministrationService) ResetPassword(context.Context, int64, string) error {
	s.calls = append(s.calls, "password")
	return s.err
}
func (s *fakeAdministrationService) AssignRole(_ context.Context, userID, roleID int64) error {
	s.calls = append(s.calls, "assign")
	s.userID, s.roleID = userID, roleID
	return s.err
}
func (s *fakeAdministrationService) RemoveRole(_ context.Context, actorID, userID, roleID int64) error {
	s.calls = append(s.calls, "remove")
	s.actorID, s.userID, s.roleID = actorID, userID, roleID
	return s.err
}
func (s *fakeAdministrationService) ListRoles(context.Context) ([]auth.Role, error) {
	s.calls = append(s.calls, "list roles")
	return s.roles, s.err
}
func (s *fakeAdministrationService) CreateRole(context.Context, string, string, authorization.Permissions) (auth.Role, error) {
	s.calls = append(s.calls, "create role")
	return auth.Role{ID: 3, Name: "editor", Permissions: authorization.Permissions{}}, s.err
}
func (s *fakeAdministrationService) UpdateRole(context.Context, int64, string, string) error {
	s.calls = append(s.calls, "update role")
	return s.err
}
func (s *fakeAdministrationService) ReplaceRolePermissions(context.Context, int64, authorization.Permissions) error {
	s.calls = append(s.calls, "permissions")
	return s.err
}

func newAdminTestRouter(service administrationService, isAdmin bool) http.Handler {
	router := NewRouter(nil, nil, nil, nil, nil, testAuthService{user: testUser()}, testPermissionProvider{permissions: adminTestPermissions(), isAdmin: isAdmin}, nil, false, service)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
		if r.Method != http.MethodGet {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
			r.Header.Set(csrfHeaderName, testCSRFToken)
		}
		router.ServeHTTP(w, r)
	})
}

func TestAdminEndpoints(t *testing.T) {
	service := &fakeAdministrationService{
		users: []auth.UserSummary{{ID: 1, Email: "admin@example.test", DisplayName: "Admin", IsActive: true, Roles: []string{"admin"}}},
		roles: []auth.Role{{ID: 1, Name: "admin", IsSystem: true, Permissions: adminTestPermissions()}},
	}
	router := newAdminTestRouter(service, true)
	for _, test := range []struct {
		name   string
		method string
		target string
		body   string
		want   int
	}{
		{name: "list users", method: http.MethodGet, target: "/api/admin/users", want: http.StatusOK},
		{name: "create user", method: http.MethodPost, target: "/api/admin/users", body: `{"email":"new@example.test","display_name":"New user","password":"long-test-password"}`, want: http.StatusCreated},
		{name: "display name", method: http.MethodPatch, target: "/api/admin/users/2/display-name", body: `{"display_name":"Renamed"}`, want: http.StatusNoContent},
		{name: "status", method: http.MethodPatch, target: "/api/admin/users/2/status", body: `{"is_active":false}`, want: http.StatusNoContent},
		{name: "password", method: http.MethodPost, target: "/api/admin/users/2/password-reset", body: `{"password":"replacement-password"}`, want: http.StatusNoContent},
		{name: "assign", method: http.MethodPut, target: "/api/admin/users/2/roles/3", want: http.StatusNoContent},
		{name: "remove", method: http.MethodDelete, target: "/api/admin/users/2/roles/3", want: http.StatusNoContent},
		{name: "list roles", method: http.MethodGet, target: "/api/admin/roles", want: http.StatusOK},
		{name: "create role", method: http.MethodPost, target: "/api/admin/roles", body: `{"name":"editor","description":"Editors","permissions":[{"schema":"public","resource":"books","operations":["read","update"]}]}`, want: http.StatusCreated},
		{name: "update role", method: http.MethodPatch, target: "/api/admin/roles/3", body: `{"name":"editor","description":"Updated"}`, want: http.StatusNoContent},
		{name: "permissions", method: http.MethodPut, target: "/api/admin/roles/3/permissions", body: `{"permissions":[{"schema":"*","resource":"*","operations":["read"]}]}`, want: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(test.method, test.target, strings.NewReader(test.body)))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.want, response.Body.String())
			}
			for _, secret := range []string{"password_hash", "token_hash", "session_token", "long-test-password", "replacement-password"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("response leaked %q", secret)
				}
			}
		})
	}
	if len(service.calls) != 11 || service.actorID != 1 || service.userID != 2 || service.roleID != 3 {
		t.Fatalf("service calls = %#v, actor/user/role = %d/%d/%d", service.calls, service.actorID, service.userID, service.roleID)
	}
}

func TestAdminAPIRequiresSessionAdminRoleAndCSRF(t *testing.T) {
	service := &fakeAdministrationService{}
	router := NewRouter(nil, nil, nil, nil, nil, testAuthService{user: testUser()}, testPermissionProvider{permissions: adminTestPermissions(), isAdmin: true}, nil, false, service)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/users", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", response.Code)
	}

	for _, isAdmin := range []bool{false} {
		viewer := newAdminTestRouter(service, isAdmin)
		response = httptest.NewRecorder()
		viewer.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/users", nil))
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "FORBIDDEN") {
			t.Fatalf("viewer status = %d, body = %s", response.Code, response.Body.String())
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/admin/users", strings.NewReader(`{"email":"new@example.test","password":"long-test-password"}`))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "CSRF_TOKEN_INVALID") {
		t.Fatalf("missing CSRF status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(service.calls) != 0 {
		t.Fatalf("unauthorized requests reached service: %#v", service.calls)
	}
}

func TestAdminValidationAndConflicts(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "validation", err: &auth.AdminValidationError{Field: "email", Message: "Invalid email"}, want: http.StatusBadRequest, code: "INVALID_ADMIN_REQUEST"},
		{name: "duplicate", err: auth.ErrUserAlreadyExists, want: http.StatusConflict, code: "ALREADY_EXISTS"},
		{name: "missing user", err: auth.ErrUserNotFound, want: http.StatusNotFound, code: "USER_NOT_FOUND"},
		{name: "last admin", err: auth.ErrLastActiveAdmin, want: http.StatusConflict, code: "LAST_ACTIVE_ADMIN"},
		{name: "self disable", err: auth.ErrSelfDisable, want: http.StatusConflict, code: "SELF_DISABLE_FORBIDDEN"},
		{name: "self role removal", err: auth.ErrSelfAdminRemoval, want: http.StatusConflict, code: "SELF_ADMIN_REMOVAL_FORBIDDEN"},
		{name: "system role", err: auth.ErrSystemRoleMutable, want: http.StatusConflict, code: "SYSTEM_ROLE_IMMUTABLE"},
		{name: "internal", err: errors.New("secret database details"), want: http.StatusInternalServerError, code: "ADMIN_OPERATION_FAILED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeAdministrationService{err: test.err}
			response := httptest.NewRecorder()
			newAdminTestRouter(service, true).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/admin/users", strings.NewReader(`{"email":"new@example.test","password":"long-test-password"}`)))
			if response.Code != test.want || !strings.Contains(response.Body.String(), test.code) {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "secret database") {
				t.Fatal("internal error leaked")
			}
		})
	}
}

func TestMeReportsAdministrationCapability(t *testing.T) {
	for _, test := range []struct {
		name    string
		isAdmin bool
	}{
		{name: "administrator", isAdmin: true},
		{name: "viewer", isAdmin: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			newAdminTestRouter(&fakeAdministrationService{}, test.isAdmin).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d", response.Code)
			}
			var body authResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.CanAdmin != test.isAdmin {
				t.Fatalf("can_admin = %t", body.CanAdmin)
			}
		})
	}
}

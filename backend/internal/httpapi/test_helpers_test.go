package httpapi

import (
	"context"
	"net/http"

	"velmir/backend/internal/auth"
	"velmir/backend/internal/authorization"
)

type testAuthService struct {
	user auth.User
}

func (s testAuthService) Login(context.Context, string, string, string) (auth.LoginResult, error) {
	return auth.LoginResult{Token: "test-session", User: s.user}, nil
}

func (s testAuthService) Authenticate(context.Context, string) (auth.User, error) {
	if s.user.ID == 0 {
		return auth.User{}, auth.ErrUnauthorized
	}
	return s.user, nil
}

func (s testAuthService) Logout(context.Context, string) error { return nil }

type testPermissionProvider struct {
	permissions authorization.Permissions
	err         error
	isAdmin     bool
}

func (s testPermissionProvider) Permissions(context.Context, int64) (authorization.Permissions, error) {
	return s.permissions, s.err
}

func (s testPermissionProvider) IsAdmin(context.Context, int64) (bool, error) {
	return s.isAdmin, s.err
}

func adminTestPermissions() authorization.Permissions {
	return authorization.EffectivePermissions([]authorization.Grant{
		{Schema: "*", Resource: "*", Operation: authorization.Read},
		{Schema: "*", Resource: "*", Operation: authorization.Create},
		{Schema: "*", Resource: "*", Operation: authorization.Update},
		{Schema: "*", Resource: "*", Operation: authorization.Delete},
	})
}

func viewerTestPermissions() authorization.Permissions {
	return authorization.EffectivePermissions([]authorization.Grant{
		{Schema: "*", Resource: "*", Operation: authorization.Read},
	})
}

func newTestRouter(adminDatabase, targetDatabase databasePinger, inspector schemaInspector, args ...any) http.Handler {
	var lister recordLister
	var writer recordWriter
	if len(args) > 0 && args[0] != nil {
		lister = args[0].(recordLister)
	}
	if len(args) > 1 && args[1] != nil {
		writer = args[1].(recordWriter)
	}
	router := NewRouter(adminDatabase, targetDatabase, inspector, lister, writer, testAuthService{user: testUser()}, testPermissionProvider{permissions: adminTestPermissions()}, nil, false)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
			r.Header.Set(csrfHeaderName, testCSRFToken)
		}
		router.ServeHTTP(w, r)
	})
}

func newTestRouterWithWriter(adminDatabase, targetDatabase databasePinger, inspector schemaInspector, lister recordLister, writer recordWriter) http.Handler {
	return newTestRouter(adminDatabase, targetDatabase, inspector, lister, writer)
}

func testUser() auth.User {
	return auth.User{ID: 1, Email: "admin@example.test", DisplayName: "Administrator", IsActive: true}
}

const testCSRFToken = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"

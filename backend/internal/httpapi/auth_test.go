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
)

type fakeAuthService struct {
	loginResult auth.LoginResult
	loginErr    error
	authErr     error
	logoutErr   error
	logoutToken string
}

func (s *fakeAuthService) Login(context.Context, string, string, string) (auth.LoginResult, error) {
	return s.loginResult, s.loginErr
}

func (s *fakeAuthService) Authenticate(context.Context, string) (auth.User, error) {
	if s.authErr != nil {
		return auth.User{}, s.authErr
	}
	return testUser(), nil
}

func (s *fakeAuthService) Logout(_ context.Context, token string) error {
	s.logoutToken = token
	return s.logoutErr
}

func TestProtectedAPIReturnsJSON401(t *testing.T) {
	service := &fakeAuthService{}
	router := NewRouter(nil, nil, nil, nil, nil, service, testPermissionProvider{permissions: adminTestPermissions()}, nil, false)
	for _, target := range []string{"/api/schema", "/api/resources/public/items/records", "/api/auth/me"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, body = %s", target, response.Code, response.Body.String())
		}
		if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") || strings.Contains(response.Body.String(), "stack") {
			t.Fatalf("%s response is not safe JSON: %s", target, response.Body.String())
		}
		var body errorResponse
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.Error.Code != "UNAUTHENTICATED" {
			t.Fatalf("%s body = %+v, error = %v", target, body, err)
		}
	}
}

func TestHealthzDoesNotRequireAuthentication(t *testing.T) {
	response := httptest.NewRecorder()
	NewRouter(nil, nil, nil, nil, nil, &fakeAuthService{authErr: auth.ErrUnauthorized}, testPermissionProvider{}, nil, false).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestLoginSetsSecureCookiesAndReturnsCSRF(t *testing.T) {
	service := &fakeAuthService{loginResult: auth.LoginResult{Token: "random-test-session-token", User: testUser()}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"admin@example.test","password":"not-logged"}`))
	NewRouter(nil, nil, nil, nil, nil, service, testPermissionProvider{permissions: adminTestPermissions()}, nil, true).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	var session, csrf *http.Cookie
	for _, cookie := range cookies {
		switch cookie.Name {
		case sessionCookieName:
			session = cookie
		case csrfCookieName:
			csrf = cookie
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.Path != "/" {
		t.Fatalf("session cookie = %+v", session)
	}
	if csrf == nil || csrf.HttpOnly || !csrf.Secure || csrf.SameSite != http.SameSiteLaxMode || !validCSRFToken(csrf.Value) {
		t.Fatalf("CSRF cookie = %+v", csrf)
	}
	var body authResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.CSRFToken != csrf.Value || body.User.Email != testUser().Email {
		t.Fatalf("body = %+v", body)
	}
	if strings.Contains(response.Body.String(), "not-logged") || strings.Contains(response.Body.String(), "random-test-session-token") {
		t.Fatal("response contains credentials or session token")
	}
}

func TestLoginUsesSameGenericCredentialError(t *testing.T) {
	var bodies []string
	for i := 0; i < 2; i++ {
		service := &fakeAuthService{loginErr: auth.ErrInvalidCredentials}
		response := httptest.NewRecorder()
		NewRouter(nil, nil, nil, nil, nil, service, testPermissionProvider{permissions: adminTestPermissions()}, nil, false).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"unknown@example.test","password":"wrong"}`)))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", response.Code)
		}
		bodies = append(bodies, response.Body.String())
	}
	if bodies[0] != bodies[1] || !strings.Contains(bodies[0], "INVALID_CREDENTIALS") {
		t.Fatalf("responses = %q", bodies)
	}
}

func TestMeRequiresSessionAndInitializesCSRF(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	NewRouter(nil, nil, nil, nil, nil, &fakeAuthService{}, testPermissionProvider{permissions: adminTestPermissions()}, nil, false).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body authResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || !validCSRFToken(body.CSRFToken) {
		t.Fatalf("body = %+v, error = %v", body, err)
	}
}

func TestMutationRequiresMatchingCSRFCookieAndHeader(t *testing.T) {
	for _, test := range []struct {
		name   string
		cookie string
		header string
	}{
		{name: "missing"},
		{name: "header only", header: testCSRFToken},
		{name: "mismatch", cookie: testCSRFToken, header: "YWJjZGVmMDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODk"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{}}`))
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
			if test.cookie != "" {
				request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: test.cookie})
			}
			if test.header != "" {
				request.Header.Set(csrfHeaderName, test.header)
			}
			NewRouter(nil, nil, recordsInspector(), nil, nil, &fakeAuthService{}, testPermissionProvider{permissions: adminTestPermissions()}, nil, false).ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "CSRF_TOKEN_INVALID") {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestLogoutRevokesCurrentSessionAndClearsCookies(t *testing.T) {
	service := &fakeAuthService{}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "current-session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	request.Header.Set(csrfHeaderName, testCSRFToken)
	NewRouter(nil, nil, nil, nil, nil, service, testPermissionProvider{permissions: adminTestPermissions()}, nil, false).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || service.logoutToken != "current-session" {
		t.Fatalf("status = %d, token = %q, body = %s", response.Code, service.logoutToken, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.MaxAge != -1 {
			t.Errorf("cookie %s MaxAge = %d", cookie.Name, cookie.MaxAge)
		}
	}
}

func TestAuthenticationFailureDoesNotLeakInternals(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	NewRouter(nil, nil, nil, nil, nil, &fakeAuthService{authErr: errors.New("secret connection details")}, testPermissionProvider{}, nil, false).ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

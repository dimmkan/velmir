package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"velmir/backend/internal/auth"
)

const (
	sessionCookieName  = "velmir_session"
	csrfCookieName     = "velmir_csrf"
	csrfHeaderName     = "X-CSRF-Token"
	authRequestTimeout = 10 * time.Second
	maxLoginBodySize   = 4096
	csrfTokenBytes     = 32
)

type authService interface {
	Login(context.Context, string, string, string) (auth.LoginResult, error)
	Authenticate(context.Context, string) (auth.User, error)
	Logout(context.Context, string) error
}

type userResponse struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type authResponse struct {
	User      userResponse `json:"user"`
	CSRFToken string       `json:"csrf_token"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type contextKey int

const userContextKey contextKey = iota

func login(service authService, secureCookies bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: apiError{Code: "AUTH_UNAVAILABLE", Message: "Authentication is not available"}})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxLoginBodySize)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request loginRequest
		if err := decoder.Decode(&request); err != nil || request.Email == "" || request.Password == "" || len(request.Email) > 254 || len(request.Password) > 1024 {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_LOGIN_REQUEST", Message: "Email and password are required"}})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_LOGIN_REQUEST", Message: "Request body must contain one JSON object"}})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), authRequestTimeout)
		defer cancel()
		result, err := service.Login(ctx, request.Email, request.Password, r.RemoteAddr)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: apiError{Code: "INVALID_CREDENTIALS", Message: "Invalid email or password"}})
			return
		}
		if errors.Is(err, auth.ErrTooManyAttempts) {
			writeJSON(w, http.StatusTooManyRequests, errorResponse{Error: apiError{Code: "AUTH_RATE_LIMITED", Message: "Too many login attempts. Try again later"}})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "LOGIN_FAILED", Message: "Could not sign in"}})
			return
		}
		csrfToken, err := newCSRFCookie(w, secureCookies)
		if err != nil {
			_ = service.Logout(ctx, result.Token)
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "LOGIN_FAILED", Message: "Could not sign in"}})
			return
		}
		setSessionCookie(w, result.Token, secureCookies)
		writeJSON(w, http.StatusOK, makeAuthResponse(result.User, csrfToken))
	}
}

func me(secureCookies bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, err := ensureCSRFCookie(w, r, secureCookies)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "CSRF_GENERATION_FAILED", Message: "Could not initialize the session"}})
			return
		}
		writeJSON(w, http.StatusOK, makeAuthResponse(currentUser(r), csrfToken))
	}
}

func logout(service authService, secureCookies bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), authRequestTimeout)
		defer cancel()
		cookie, _ := r.Cookie(sessionCookieName)
		if err := service.Logout(ctx, cookie.Value); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "LOGOUT_FAILED", Message: "Could not sign out"}})
			return
		}
		clearAuthCookies(w, secureCookies)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

func requireAuth(service authService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil || service == nil {
				writeUnauthenticated(w)
				return
			}
			user, err := service.Authenticate(r.Context(), cookie.Value)
			if errors.Is(err, auth.ErrUnauthorized) {
				writeUnauthenticated(w)
				return
			}
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "AUTH_CHECK_FAILED", Message: "Could not verify authentication"}})
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(csrfCookieName)
		header := r.Header.Get(csrfHeaderName)
		if err != nil || !validCSRFToken(cookie.Value) || !validCSRFToken(header) {
			writeCSRFError(w)
			return
		}
		cookieHash := sha256.Sum256([]byte(cookie.Value))
		headerHash := sha256.Sum256([]byte(header))
		if subtle.ConstantTimeCompare(cookieHash[:], headerHash[:]) != 1 {
			writeCSRFError(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func makeAuthResponse(user auth.User, csrfToken string) authResponse {
	return authResponse{User: userResponse{ID: user.ID, Email: user.Email, DisplayName: user.DisplayName}, CSRFToken: csrfToken}
}

func currentUser(r *http.Request) auth.User {
	user, _ := r.Context().Value(userContextKey).(auth.User)
	return user
}

func newCSRFCookie(w http.ResponseWriter, secure bool) (string, error) {
	bytes := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	setCSRFCookie(w, token, secure)
	return token, nil
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request, secure bool) (string, error) {
	cookie, err := r.Cookie(csrfCookieName)
	if err == nil && validCSRFToken(cookie.Value) {
		return cookie.Value, nil
	}
	return newCSRFCookie(w, secure)
}

func validCSRFToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == csrfTokenBytes
}

func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func setCSRFCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: token, Path: "/", HttpOnly: false,
		Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func clearAuthCookies(w http.ResponseWriter, secure bool) {
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", HttpOnly: name == sessionCookieName,
			Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0),
		})
	}
}

func writeUnauthenticated(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, errorResponse{Error: apiError{Code: "UNAUTHENTICATED", Message: "Authentication required"}})
}

func writeCSRFError(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, errorResponse{Error: apiError{Code: "CSRF_TOKEN_INVALID", Message: "CSRF token is missing or invalid"}})
}

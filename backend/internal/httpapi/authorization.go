package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"velmir/backend/internal/authorization"
)

const authorizationTimeout = 10 * time.Second

type permissionProvider interface {
	Permissions(context.Context, int64) (authorization.Permissions, error)
}

type adminRoleProvider interface {
	IsAdmin(context.Context, int64) (bool, error)
}

func canAdminister(ctx context.Context, provider permissionProvider, userID int64) (bool, error) {
	roles, ok := provider.(adminRoleProvider)
	if !ok {
		return false, nil
	}
	return roles.IsAdmin(ctx, userID)
}

func requireAdmin(provider permissionProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), authorizationTimeout)
			defer cancel()
			isAdmin, err := canAdminister(ctx, provider, currentUser(r).ID)
			if err != nil {
				writeAuthorizationCheckFailed(w)
				return
			}
			if !isAdmin {
				writeForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type permissionsContextKey struct{}

func loadPermissions(provider permissionProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if provider == nil {
				writeAuthorizationCheckFailed(w)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), authorizationTimeout)
			defer cancel()
			permissions, err := provider.Permissions(ctx, currentUser(r).ID)
			if err != nil {
				writeAuthorizationCheckFailed(w)
				return
			}
			ctx = context.WithValue(ctx, permissionsContextKey{}, permissions)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requireAnyPermission(operation authorization.Operation) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !currentPermissions(r).HasAny(operation) {
				writeForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requireResourcePermission(operation authorization.Operation) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !currentPermissions(r).Allows(chi.URLParam(r, "schema"), chi.URLParam(r, "resource"), operation) {
				writeForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func currentPermissions(r *http.Request) authorization.Permissions {
	permissions, _ := r.Context().Value(permissionsContextKey{}).(authorization.Permissions)
	if permissions == nil {
		return make(authorization.Permissions, 0)
	}
	return permissions
}

func writeForbidden(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, errorResponse{Error: apiError{Code: "FORBIDDEN", Message: "You do not have permission to access this resource"}})
}

func writeAuthorizationCheckFailed(w http.ResponseWriter) {
	writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "AUTHORIZATION_CHECK_FAILED", Message: "Could not verify permissions"}})
}

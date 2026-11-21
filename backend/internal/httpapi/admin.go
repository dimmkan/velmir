package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"velmir/backend/internal/auth"
	"velmir/backend/internal/authorization"
)

const (
	adminRequestTimeout = 10 * time.Second
	maxAdminBodySize    = 1024 * 1024
)

type administrationService interface {
	ListUsers(context.Context) ([]auth.UserSummary, error)
	CreateUser(context.Context, string, string, string) (auth.UserSummary, error)
	UpdateDisplayName(context.Context, int64, string) error
	SetUserActive(context.Context, int64, int64, bool) error
	ResetPassword(context.Context, int64, string) error
	AssignRole(context.Context, int64, int64) error
	RemoveRole(context.Context, int64, int64, int64) error
	ListRoles(context.Context) ([]auth.Role, error)
	CreateRole(context.Context, string, string, authorization.Permissions) (auth.Role, error)
	UpdateRole(context.Context, int64, string, string) error
	ReplaceRolePermissions(context.Context, int64, authorization.Permissions) error
}

type adminUserResponse struct {
	ID          int64    `json:"id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	IsActive    bool     `json:"is_active"`
	Roles       []string `json:"roles"`
}

type adminRoleResponse struct {
	ID          int64                     `json:"id"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	IsSystem    bool                      `json:"is_system"`
	Permissions authorization.Permissions `json:"permissions"`
}

type createUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

type displayNameRequest struct {
	DisplayName string `json:"display_name"`
}

type userStatusRequest struct {
	IsActive *bool `json:"is_active"`
}

type passwordResetRequest struct {
	Password string `json:"password"`
}

type roleRequest struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Permissions authorization.Permissions `json:"permissions"`
}

type updateRoleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type permissionsRequest struct {
	Permissions authorization.Permissions `json:"permissions"`
}

func adminUsers(service administrationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeAdminUnavailable(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		users, err := service.ListUsers(ctx)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		response := make([]adminUserResponse, 0, len(users))
		for _, user := range users {
			response = append(response, makeAdminUserResponse(user))
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": response})
	}
}

func createAdminUser(service administrationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request createUserRequest
		if !decodeAdminRequest(w, r, &request) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		user, err := service.CreateUser(ctx, request.Email, request.DisplayName, request.Password)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"user": makeAdminUserResponse(user)})
	}
}

func updateAdminDisplayName(service administrationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := adminPathID(w, r, "userID", "USER_NOT_FOUND")
		if !ok {
			return
		}
		var request displayNameRequest
		if !decodeAdminRequest(w, r, &request) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		if err := service.UpdateDisplayName(ctx, userID, request.DisplayName); err != nil {
			writeAdminError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func setAdminUserStatus(service administrationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := adminPathID(w, r, "userID", "USER_NOT_FOUND")
		if !ok {
			return
		}
		var request userStatusRequest
		if !decodeAdminRequest(w, r, &request) {
			return
		}
		if request.IsActive == nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_ADMIN_REQUEST", Message: "is_active is required", FieldErrors: map[string]string{"is_active": "Required"}}})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		if err := service.SetUserActive(ctx, currentUser(r).ID, userID, *request.IsActive); err != nil {
			writeAdminError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func resetAdminPassword(service administrationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := adminPathID(w, r, "userID", "USER_NOT_FOUND")
		if !ok {
			return
		}
		var request passwordResetRequest
		if !decodeAdminRequest(w, r, &request) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		if err := service.ResetPassword(ctx, userID, request.Password); err != nil {
			writeAdminError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func assignAdminRole(service administrationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, roleID, ok := adminAssignmentIDs(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		if err := service.AssignRole(ctx, userID, roleID); err != nil {
			writeAdminError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func removeAdminRole(service administrationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, roleID, ok := adminAssignmentIDs(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		if err := service.RemoveRole(ctx, currentUser(r).ID, userID, roleID); err != nil {
			writeAdminError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func adminRoles(service administrationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeAdminUnavailable(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		roles, err := service.ListRoles(ctx)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		response := make([]adminRoleResponse, 0, len(roles))
		for _, role := range roles {
			response = append(response, makeAdminRoleResponse(role))
		}
		writeJSON(w, http.StatusOK, map[string]any{"roles": response})
	}
}

func createAdminRole(service administrationService, inspector schemaInspector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request roleRequest
		if !decodeAdminRequest(w, r, &request) {
			return
		}
		if request.Permissions == nil {
			request.Permissions = make(authorization.Permissions, 0)
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		if !validateAdminPermissionMetadata(ctx, w, inspector, request.Permissions) {
			return
		}
		role, err := service.CreateRole(ctx, request.Name, request.Description, request.Permissions)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"role": makeAdminRoleResponse(role)})
	}
}

func updateAdminRole(service administrationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roleID, ok := adminPathID(w, r, "roleID", "ROLE_NOT_FOUND")
		if !ok {
			return
		}
		var request updateRoleRequest
		if !decodeAdminRequest(w, r, &request) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		if err := service.UpdateRole(ctx, roleID, request.Name, request.Description); err != nil {
			writeAdminError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func replaceAdminRolePermissions(service administrationService, inspector schemaInspector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roleID, ok := adminPathID(w, r, "roleID", "ROLE_NOT_FOUND")
		if !ok {
			return
		}
		var request permissionsRequest
		if !decodeAdminRequest(w, r, &request) {
			return
		}
		if request.Permissions == nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_ADMIN_REQUEST", Message: "permissions must be an array", FieldErrors: map[string]string{"permissions": "Required"}}})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), adminRequestTimeout)
		defer cancel()
		if !validateAdminPermissionMetadata(ctx, w, inspector, request.Permissions) {
			return
		}
		if err := service.ReplaceRolePermissions(ctx, roleID, request.Permissions); err != nil {
			writeAdminError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func validateAdminPermissionMetadata(ctx context.Context, w http.ResponseWriter, inspector schemaInspector, permissions authorization.Permissions) bool {
	hasFieldPermissions := false
	for _, permission := range permissions {
		hasFieldPermissions = hasFieldPermissions || len(permission.FieldPermissions) > 0
	}
	if !hasFieldPermissions {
		return true
	}
	if err := authorization.ValidatePermissions(permissions); err != nil {
		writeAdminError(w, &auth.AdminValidationError{Field: "permissions", Message: err.Error()})
		return false
	}
	if inspector == nil {
		writeTargetNotConfigured(w)
		return false
	}
	catalog, err := inspector.Inspect(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "SCHEMA_INTROSPECTION_FAILED", Message: "Could not read target database schema"}})
		return false
	}
	if err := authorization.ValidatePermissionColumns(permissions, catalog); err != nil {
		writeAdminError(w, &auth.AdminValidationError{Field: "permissions", Message: err.Error()})
		return false
	}
	return true
}

func decodeAdminRequest(w http.ResponseWriter, r *http.Request, destination any) bool {
	if serviceRequestUnavailable(w, r) {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_ADMIN_REQUEST", Message: "Request body must be a valid JSON object"}})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_ADMIN_REQUEST", Message: "Request body must contain one JSON object"}})
		return false
	}
	return true
}

func serviceRequestUnavailable(w http.ResponseWriter, r *http.Request) bool {
	service := r.Context().Value(adminServiceContextKey{})
	if service == nil {
		writeAdminUnavailable(w)
		return true
	}
	return false
}

type adminServiceContextKey struct{}

func withAdminService(service administrationService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), adminServiceContextKey{}, service)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func adminPathID(w http.ResponseWriter, r *http.Request, name, code string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: apiError{Code: code, Message: "The requested item was not found"}})
		return 0, false
	}
	return id, true
}

func adminAssignmentIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	userID, ok := adminPathID(w, r, "userID", "USER_NOT_FOUND")
	if !ok {
		return 0, 0, false
	}
	roleID, ok := adminPathID(w, r, "roleID", "ROLE_NOT_FOUND")
	return userID, roleID, ok
}

func makeAdminUserResponse(user auth.UserSummary) adminUserResponse {
	if user.Roles == nil {
		user.Roles = []string{}
	}
	return adminUserResponse{ID: user.ID, Email: user.Email, DisplayName: user.DisplayName, IsActive: user.IsActive, Roles: user.Roles}
}

func makeAdminRoleResponse(role auth.Role) adminRoleResponse {
	if role.Permissions == nil {
		role.Permissions = make(authorization.Permissions, 0)
	}
	return adminRoleResponse{ID: role.ID, Name: role.Name, Description: role.Description, IsSystem: role.IsSystem, Permissions: role.Permissions}
}

func writeAdminError(w http.ResponseWriter, err error) {
	var validation *auth.AdminValidationError
	switch {
	case errors.As(err, &validation):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_ADMIN_REQUEST", Message: validation.Message, FieldErrors: map[string]string{validation.Field: validation.Message}}})
	case errors.Is(err, auth.ErrUserAlreadyExists), errors.Is(err, auth.ErrRoleAlreadyExists):
		writeJSON(w, http.StatusConflict, errorResponse{Error: apiError{Code: "ALREADY_EXISTS", Message: "A user or role with this unique value already exists"}})
	case errors.Is(err, auth.ErrUserNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: apiError{Code: "USER_NOT_FOUND", Message: "User not found"}})
	case errors.Is(err, auth.ErrRoleNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: apiError{Code: "ROLE_NOT_FOUND", Message: "Role not found"}})
	case errors.Is(err, auth.ErrLastActiveAdmin):
		writeJSON(w, http.StatusConflict, errorResponse{Error: apiError{Code: "LAST_ACTIVE_ADMIN", Message: "The last active administrator cannot be disabled or lose the admin role"}})
	case errors.Is(err, auth.ErrSelfDisable):
		writeJSON(w, http.StatusConflict, errorResponse{Error: apiError{Code: "SELF_DISABLE_FORBIDDEN", Message: "You cannot disable your own account"}})
	case errors.Is(err, auth.ErrSelfAdminRemoval):
		writeJSON(w, http.StatusConflict, errorResponse{Error: apiError{Code: "SELF_ADMIN_REMOVAL_FORBIDDEN", Message: "You cannot remove your own admin role"}})
	case errors.Is(err, auth.ErrSystemRoleMutable):
		writeJSON(w, http.StatusConflict, errorResponse{Error: apiError{Code: "SYSTEM_ROLE_IMMUTABLE", Message: "System roles cannot be renamed or have their permissions changed"}})
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_ADMIN_REQUEST", Message: "Invalid user credentials"}})
	default:
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "ADMIN_OPERATION_FAILED", Message: "Could not complete the administration operation"}})
	}
}

func writeAdminUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: apiError{Code: "ADMIN_UNAVAILABLE", Message: "Administration is not available"}})
}

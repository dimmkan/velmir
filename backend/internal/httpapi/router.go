package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"velmir/backend/internal/audit"
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

const readinessTimeout = 2 * time.Second
const schemaTimeout = 10 * time.Second
const recordsTimeout = 10 * time.Second
const mutationTimeout = 10 * time.Second
const historyTimeout = 10 * time.Second
const maxMutationBodySize = 1024 * 1024
const defaultHistoryLimit = 20
const maxHistoryLimit = 100

type databasePinger interface {
	Ping(context.Context) error
}

type schemaInspector interface {
	Inspect(context.Context) (introspection.Catalog, error)
}

type recordLister interface {
	List(context.Context, string, introspection.Resource, records.ListOptions) (records.ListResult, error)
}

type recordWriter interface {
	Create(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error)
	Update(context.Context, string, introspection.Resource, string, records.RecordValues) (records.UpdateResult, error)
	Delete(context.Context, string, introspection.Resource, string) (map[string]any, error)
}

type recordAudit interface {
	Record(context.Context, audit.Input) error
	History(context.Context, string, string, json.RawMessage, int64, int) (audit.HistoryResult, error)
}

type readinessResponse struct {
	Status         string `json:"status"`
	AdminDatabase  string `json:"admin_database"`
	TargetDatabase string `json:"target_database"`
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code              string            `json:"code"`
	Message           string            `json:"message"`
	FieldErrors       map[string]string `json:"field_errors,omitempty"`
	MutationCommitted bool              `json:"mutation_committed,omitempty"`
}

type mutationRequest struct {
	Values records.RecordValues `json:"values"`
}

func NewRouter(adminDatabase, targetDatabase databasePinger, inspector schemaInspector, lister recordLister, writer recordWriter, authService authService, permissions permissionProvider, auditStore recordAudit, secureCookies bool, adminServices ...administrationService) http.Handler {
	var administration administrationService
	if len(adminServices) > 0 {
		administration = adminServices[0]
	}
	router := chi.NewRouter()
	router.Get("/healthz", healthz)
	router.Get("/readyz", readyz(adminDatabase, targetDatabase))
	router.Post("/api/auth/login", login(authService, permissions, secureCookies))
	router.Group(func(r chi.Router) {
		r.Use(requireAuth(authService))
		r.Use(loadPermissions(permissions))
		r.Get("/api/auth/me", me(permissions, secureCookies))
		r.With(requireCSRF).Post("/api/auth/logout", logout(authService, secureCookies))
		r.With(requireAnyPermission(authorization.Read)).Get("/api/schema", schema(inspector, writer != nil))
		r.With(requireResourcePermission(authorization.Read)).Get("/api/resources/{schema}/{resource}/records", listRecords(inspector, lister))
		r.With(requireResourcePermission(authorization.Read)).Post("/api/resources/{schema}/{resource}/relations/resolve", resolveRelations(inspector, lister))
		r.With(requireResourcePermission(authorization.Read)).Get("/api/resources/{schema}/{resource}/records/{key}/history", recordHistory(inspector, auditStore))
		r.With(requireCSRF, requireResourcePermission(authorization.Create)).Post("/api/resources/{schema}/{resource}/records", createRecord(inspector, writer, auditStore))
		r.With(requireCSRF, requireResourcePermission(authorization.Update)).Patch("/api/resources/{schema}/{resource}/records/{key}", updateRecord(inspector, writer, auditStore))
		r.With(requireCSRF, requireResourcePermission(authorization.Delete)).Delete("/api/resources/{schema}/{resource}/records/{key}", deleteRecord(inspector, writer, auditStore))
		r.Route("/api/admin", func(admin chi.Router) {
			admin.Use(requireAdmin(permissions))
			admin.Use(withAdminService(administration))
			admin.Get("/users", adminUsers(administration))
			admin.With(requireCSRF).Post("/users", createAdminUser(administration))
			admin.With(requireCSRF).Patch("/users/{userID}/display-name", updateAdminDisplayName(administration))
			admin.With(requireCSRF).Patch("/users/{userID}/status", setAdminUserStatus(administration))
			admin.With(requireCSRF).Post("/users/{userID}/password-reset", resetAdminPassword(administration))
			admin.With(requireCSRF).Put("/users/{userID}/roles/{roleID}", assignAdminRole(administration))
			admin.With(requireCSRF).Delete("/users/{userID}/roles/{roleID}", removeAdminRole(administration))
			admin.Get("/roles", adminRoles(administration))
			admin.With(requireCSRF).Post("/roles", createAdminRole(administration, inspector))
			admin.With(requireCSRF).Patch("/roles/{roleID}", updateAdminRole(administration))
			admin.With(requireCSRF).Put("/roles/{roleID}/permissions", replaceAdminRolePermissions(administration, inspector))
		})
	})
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: apiError{Code: "NOT_FOUND", Message: "Endpoint not found"}})
	})
	return router
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func readyz(adminDatabase, targetDatabase databasePinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		response := readinessResponse{
			Status:         "ready",
			AdminDatabase:  databaseStatus(ctx, adminDatabase),
			TargetDatabase: databaseStatus(ctx, targetDatabase),
		}
		statusCode := http.StatusOK
		if response.AdminDatabase != "ready" || response.TargetDatabase == "unavailable" {
			response.Status = "not_ready"
			statusCode = http.StatusServiceUnavailable
		}

		writeJSON(w, statusCode, response)
	}
}

func schema(inspector schemaInspector, writeEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if inspector == nil {
			writeTargetNotConfigured(w)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), schemaTimeout)
		defer cancel()
		catalog, err := inspector.Inspect(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{
				Code:    "SCHEMA_INTROSPECTION_FAILED",
				Message: "Could not read target database schema",
			}})
			return
		}
		permissions := currentPermissions(r)
		catalog = filterReadableCatalog(catalog, permissions)
		for schemaIndex := range catalog.Schemas {
			for resourceIndex := range catalog.Schemas[schemaIndex].Resources {
				applyFieldPermissions(catalog.Schemas[schemaIndex].Name, &catalog.Schemas[schemaIndex].Resources[resourceIndex], permissions, writeEnabled)
			}
		}
		catalog.WriteEnabled = writeEnabled && catalogHasRealWrite(catalog, permissions)
		writeJSON(w, http.StatusOK, catalog)
	}
}

func filterReadableCatalog(catalog introspection.Catalog, permissions authorization.Permissions) introspection.Catalog {
	filtered := introspection.Catalog{Schemas: make([]introspection.Schema, 0)}
	for _, schema := range catalog.Schemas {
		schemaCopy := introspection.Schema{Name: schema.Name, Resources: make([]introspection.Resource, 0)}
		for _, resource := range schema.Resources {
			if !permissions.Allows(schema.Name, resource.Name, authorization.Read) {
				continue
			}
			resource.ForeignKeys = filterReadableForeignKeys(resource.ForeignKeys, permissions)
			resource.ReverseRelations = make([]introspection.ReverseRelation, 0)
			schemaCopy.Resources = append(schemaCopy.Resources, resource)
		}
		if len(schemaCopy.Resources) > 0 {
			filtered.Schemas = append(filtered.Schemas, schemaCopy)
		}
	}
	for _, schema := range filtered.Schemas {
		for _, child := range schema.Resources {
			for _, foreignKey := range child.ForeignKeys {
				for parentSchemaIndex := range filtered.Schemas {
					parentSchema := &filtered.Schemas[parentSchemaIndex]
					if parentSchema.Name != foreignKey.ReferencedSchema {
						continue
					}
					for parentIndex := range parentSchema.Resources {
						parent := &parentSchema.Resources[parentIndex]
						if parent.Name != foreignKey.ReferencedTable {
							continue
						}
						parent.ReverseRelations = append(parent.ReverseRelations, introspection.ReverseRelation{
							Name:              foreignKey.Name,
							Schema:            schema.Name,
							Resource:          child.Name,
							Columns:           append([]string(nil), foreignKey.Columns...),
							ReferencedColumns: append([]string(nil), foreignKey.ReferencedColumns...),
						})
					}
				}
			}
		}
	}
	return filtered
}

func filterReadableForeignKeys(foreignKeys []introspection.ForeignKey, permissions authorization.Permissions) []introspection.ForeignKey {
	filtered := make([]introspection.ForeignKey, 0, len(foreignKeys))
	for _, foreignKey := range foreignKeys {
		if permissions.Allows(foreignKey.ReferencedSchema, foreignKey.ReferencedTable, authorization.Read) {
			filtered = append(filtered, foreignKey)
		}
	}
	return filtered
}

func listRecords(inspector schemaInspector, lister recordLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if inspector == nil || lister == nil {
			writeTargetNotConfigured(w)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), recordsTimeout)
		defer cancel()
		catalog, err := inspector.Inspect(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{
				Code:    "SCHEMA_INTROSPECTION_FAILED",
				Message: "Could not read target database schema",
			}})
			return
		}
		resource, ok := catalog.FindResource(chi.URLParam(r, "schema"), chi.URLParam(r, "resource"))
		if !ok {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: apiError{
				Code:    "RESOURCE_NOT_FOUND",
				Message: "Resource is not present in the target database schema",
			}})
			return
		}
		options, err := records.ParseListOptions(r.URL.Query(), resource)
		if err != nil {
			var validationErr *records.ValidationError
			if errors.As(err, &validationErr) {
				writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{
					Code:    validationErr.Code,
					Message: validationErr.Message,
				}})
				return
			}
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{
				Code:    "INVALID_QUERY",
				Message: "Invalid records query",
			}})
			return
		}

		result, err := lister.List(ctx, chi.URLParam(r, "schema"), resource, options)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{
				Code:    "RECORDS_QUERY_FAILED",
				Message: "Could not read resource records",
			}})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func createRecord(inspector schemaInspector, writer recordWriter, auditStore recordAudit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if inspector == nil {
			writeTargetNotConfigured(w)
			return
		}
		if writer == nil {
			writeTargetReadOnly(w)
			return
		}
		values, ok := decodeMutationRequest(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), mutationTimeout)
		defer cancel()
		resource, ok := mutationResource(ctx, w, r, inspector)
		if !ok {
			return
		}
		if !enforceMutationFields(w, r, resource, authorization.Create, values) {
			return
		}
		schemaName := chi.URLParam(r, "schema")
		record, err := writer.Create(ctx, schemaName, resource, values)
		if err != nil {
			writeMutationError(w, err)
			return
		}
		if err := recordMutationAudit(ctx, auditStore, currentUser(r).ID, audit.Create, schemaName, resource, nil, record); err != nil {
			writeCommittedAuditError(w, r, audit.Create, schemaName, resource.Name, record, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"record": record})
	}
}

func updateRecord(inspector schemaInspector, writer recordWriter, auditStore recordAudit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if inspector == nil {
			writeTargetNotConfigured(w)
			return
		}
		if writer == nil {
			writeTargetReadOnly(w)
			return
		}
		values, ok := decodeMutationRequest(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), mutationTimeout)
		defer cancel()
		resource, ok := mutationResource(ctx, w, r, inspector)
		if !ok {
			return
		}
		key, err := url.PathUnescape(chi.URLParam(r, "key"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_RECORD_KEY", Message: "Record key is not valid URL-encoded JSON"}})
			return
		}
		if !enforceMutationFields(w, r, resource, authorization.Update, values) {
			return
		}
		schemaName := chi.URLParam(r, "schema")
		result, err := writer.Update(ctx, schemaName, resource, key, values)
		if err != nil {
			writeMutationError(w, err)
			return
		}
		if err := recordMutationAudit(ctx, auditStore, currentUser(r).ID, audit.Update, schemaName, resource, result.Before, result.After); err != nil {
			writeCommittedAuditError(w, r, audit.Update, schemaName, resource.Name, result.After, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"record": result.After})
	}
}

func deleteRecord(inspector schemaInspector, writer recordWriter, auditStore recordAudit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if inspector == nil {
			writeTargetNotConfigured(w)
			return
		}
		if writer == nil {
			writeTargetReadOnly(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), mutationTimeout)
		defer cancel()
		resource, ok := mutationResource(ctx, w, r, inspector)
		if !ok {
			return
		}
		key, err := url.PathUnescape(chi.URLParam(r, "key"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_RECORD_KEY", Message: "Record key is not valid URL-encoded JSON"}})
			return
		}
		schemaName := chi.URLParam(r, "schema")
		before, err := writer.Delete(ctx, schemaName, resource, key)
		if err != nil {
			writeMutationError(w, err)
			return
		}
		if err := recordMutationAudit(ctx, auditStore, currentUser(r).ID, audit.Delete, schemaName, resource, before, nil); err != nil {
			writeCommittedAuditError(w, r, audit.Delete, schemaName, resource.Name, nil, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusNoContent)
	}
}

func recordMutationAudit(ctx context.Context, auditStore recordAudit, actorUserID int64, action audit.Action, schemaName string, resource introspection.Resource, before, after map[string]any) error {
	if auditStore == nil {
		return nil
	}
	keyRecord := after
	if action == audit.Delete {
		keyRecord = before
	}
	key, err := audit.RecordKey(resource, keyRecord)
	if err != nil {
		return err
	}
	return auditStore.Record(ctx, audit.Input{
		ActorUserID: actorUserID,
		Action:      action,
		Schema:      schemaName,
		Resource:    resource.Name,
		RecordKey:   key,
		Before:      before,
		After:       after,
	})
}

func writeCommittedAuditError(w http.ResponseWriter, r *http.Request, action audit.Action, schemaName, resourceName string, record map[string]any, err error) {
	log.Printf("audit write failed after committed target mutation: action=%s schema=%q resource=%q actor_user_id=%d error_type=%T", action, schemaName, resourceName, currentUser(r).ID, err)
	message := "The target record was saved, but its audit event could not be recorded"
	if action == audit.Delete {
		message = "The target record was deleted, but its audit event could not be recorded"
	}
	body := map[string]any{
		"error": apiError{
			Code:              "AUDIT_WRITE_FAILED",
			Message:           message,
			MutationCommitted: true,
		},
	}
	if action != audit.Delete {
		body["record"] = record
	}
	writeJSON(w, http.StatusInternalServerError, body)
}

func recordHistory(inspector schemaInspector, auditStore recordAudit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if inspector == nil {
			writeTargetNotConfigured(w)
			return
		}
		if auditStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: apiError{Code: "AUDIT_UNAVAILABLE", Message: "Audit history is not available"}})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), historyTimeout)
		defer cancel()
		resource, ok := mutationResource(ctx, w, r, inspector)
		if !ok {
			return
		}
		rawKey, err := url.PathUnescape(chi.URLParam(r, "key"))
		if err != nil {
			writeInvalidHistoryKey(w)
			return
		}
		key, err := records.CanonicalRecordKey(resource, rawKey)
		if err != nil {
			writeInvalidHistoryKey(w)
			return
		}
		page, limit, ok := historyPagination(w, r)
		if !ok {
			return
		}
		result, err := auditStore.History(ctx, chi.URLParam(r, "schema"), resource.Name, key, page, limit)
		if err != nil {
			log.Printf("audit history query failed: schema=%q resource=%q error_type=%T", chi.URLParam(r, "schema"), resource.Name, err)
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "HISTORY_QUERY_FAILED", Message: "Could not read record history"}})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func historyPagination(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	page := int64(1)
	limit := defaultHistoryLimit
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || page <= 0 {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_PAGE", Message: "Page must be a positive integer"}})
			return 0, 0, false
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maxHistoryLimit {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_LIMIT", Message: "Limit must be between 1 and 100"}})
			return 0, 0, false
		}
	}
	if page > math.MaxInt64/int64(limit) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_PAGE", Message: "Page is too large"}})
		return 0, 0, false
	}
	return page, limit, true
}

func writeInvalidHistoryKey(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_RECORD_KEY", Message: "Record key must be URL-encoded JSON containing exactly the primary key fields"}})
}

func decodeMutationRequest(w http.ResponseWriter, r *http.Request) (records.RecordValues, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMutationBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request mutationRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_RECORD", Message: "Request body must contain a values object"}})
		return nil, false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{Code: "INVALID_RECORD", Message: "Request body must contain one JSON object"}})
		return nil, false
	}
	if request.Values == nil {
		request.Values = make(records.RecordValues)
	}
	return request.Values, true
}

func mutationResource(ctx context.Context, w http.ResponseWriter, r *http.Request, inspector schemaInspector) (introspection.Resource, bool) {
	catalog, err := inspector.Inspect(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "SCHEMA_INTROSPECTION_FAILED", Message: "Could not read target database schema"}})
		return introspection.Resource{}, false
	}
	resource, ok := catalog.FindResource(chi.URLParam(r, "schema"), chi.URLParam(r, "resource"))
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: apiError{Code: "RESOURCE_NOT_FOUND", Message: "Resource is not present in the target database schema"}})
		return introspection.Resource{}, false
	}
	return resource, true
}

func writeMutationError(w http.ResponseWriter, err error) {
	var mutationErr *records.MutationError
	if errors.As(err, &mutationErr) {
		status := http.StatusBadRequest
		switch mutationErr.Code {
		case "RESOURCE_READ_ONLY", "UNIQUE_VIOLATION":
			status = http.StatusConflict
		case "RECORD_NOT_FOUND":
			status = http.StatusNotFound
		case "FIELD_FORBIDDEN", "NO_WRITABLE_FIELDS", "CREATE_IMPOSSIBLE":
			status = http.StatusForbidden
		}
		writeJSON(w, status, errorResponse{Error: apiError{Code: mutationErr.Code, Message: mutationErr.Message, FieldErrors: mutationErr.FieldErrors}})
		return
	}
	writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "RECORD_MUTATION_FAILED", Message: "Could not save the record"}})
}

func writeTargetReadOnly(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, errorResponse{Error: apiError{Code: "TARGET_READ_ONLY", Message: "Target mutations are unavailable for this configuration or resource"}})
}

func writeTargetNotConfigured(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: apiError{
		Code:    "TARGET_NOT_CONFIGURED",
		Message: "Target database is not configured",
	}})
}

func databaseStatus(ctx context.Context, database databasePinger) string {
	if database == nil {
		return "not_configured"
	}
	if err := database.Ping(ctx); err != nil {
		return "unavailable"
	}
	return "ready"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

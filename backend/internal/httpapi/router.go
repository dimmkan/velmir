package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

const readinessTimeout = 2 * time.Second
const schemaTimeout = 10 * time.Second
const recordsTimeout = 10 * time.Second
const mutationTimeout = 10 * time.Second
const maxMutationBodySize = 1024 * 1024

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
	Update(context.Context, string, introspection.Resource, string, records.RecordValues) (map[string]any, error)
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
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	FieldErrors map[string]string `json:"field_errors,omitempty"`
}

type mutationRequest struct {
	Values records.RecordValues `json:"values"`
}

func NewRouter(adminDatabase, targetDatabase databasePinger, inspector schemaInspector, lister recordLister, writer recordWriter, authService authService, secureCookies bool) http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", healthz)
	router.Get("/readyz", readyz(adminDatabase, targetDatabase))
	router.Post("/api/auth/login", login(authService, secureCookies))
	router.Group(func(r chi.Router) {
		r.Use(requireAuth(authService))
		r.Get("/api/auth/me", me(secureCookies))
		r.With(requireCSRF).Post("/api/auth/logout", logout(authService, secureCookies))
		r.Get("/api/schema", schema(inspector, writer != nil))
		r.Get("/api/resources/{schema}/{resource}/records", listRecords(inspector, lister))
		r.With(requireCSRF).Post("/api/resources/{schema}/{resource}/records", createRecord(inspector, writer))
		r.With(requireCSRF).Patch("/api/resources/{schema}/{resource}/records/{key}", updateRecord(inspector, writer))
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
		catalog.WriteEnabled = writeEnabled
		writeJSON(w, http.StatusOK, catalog)
	}
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

func createRecord(inspector schemaInspector, writer recordWriter) http.HandlerFunc {
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
		record, err := writer.Create(ctx, chi.URLParam(r, "schema"), resource, values)
		if err != nil {
			writeMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"record": record})
	}
}

func updateRecord(inspector schemaInspector, writer recordWriter) http.HandlerFunc {
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
		record, err := writer.Update(ctx, chi.URLParam(r, "schema"), resource, key, values)
		if err != nil {
			writeMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"record": record})
	}
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
		}
		writeJSON(w, status, errorResponse{Error: apiError{Code: mutationErr.Code, Message: mutationErr.Message, FieldErrors: mutationErr.FieldErrors}})
		return
	}
	writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{Code: "RECORD_MUTATION_FAILED", Message: "Could not save the record"}})
}

func writeTargetReadOnly(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, errorResponse{Error: apiError{Code: "TARGET_READ_ONLY", Message: "Target writing is not configured"}})
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

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

const readinessTimeout = 2 * time.Second
const schemaTimeout = 10 * time.Second
const recordsTimeout = 10 * time.Second

type databasePinger interface {
	Ping(context.Context) error
}

type schemaInspector interface {
	Inspect(context.Context) (introspection.Catalog, error)
}

type recordLister interface {
	List(context.Context, string, introspection.Resource, records.ListOptions) (records.ListResult, error)
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
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewRouter(adminDatabase, targetDatabase databasePinger, inspector schemaInspector, listers ...recordLister) http.Handler {
	var lister recordLister
	if len(listers) > 0 {
		lister = listers[0]
	}
	router := chi.NewRouter()
	router.Get("/healthz", healthz)
	router.Get("/readyz", readyz(adminDatabase, targetDatabase))
	router.Get("/api/schema", schema(inspector))
	router.Get("/api/resources/{schema}/{resource}/records", listRecords(inspector, lister))
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

func schema(inspector schemaInspector) http.HandlerFunc {
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const readinessTimeout = 2 * time.Second

type databasePinger interface {
	Ping(context.Context) error
}

type readinessResponse struct {
	Status         string `json:"status"`
	AdminDatabase  string `json:"admin_database"`
	TargetDatabase string `json:"target_database"`
}

func NewRouter(adminDatabase, targetDatabase databasePinger) http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", healthz)
	router.Get("/readyz", readyz(adminDatabase, targetDatabase))
	return router
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(response)
	}
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

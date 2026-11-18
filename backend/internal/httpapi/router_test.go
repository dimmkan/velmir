package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type pingFunc func(context.Context) error

func (fn pingFunc) Ping(ctx context.Context) error {
	return fn(ctx)
}

func TestHealthz(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status body = %q, want ok", body["status"])
	}
}

func TestReadyz(t *testing.T) {
	readyDatabase := pingFunc(func(context.Context) error { return nil })
	unavailableDatabase := pingFunc(func(context.Context) error { return errors.New("unavailable") })

	tests := []struct {
		name           string
		admin          databasePinger
		target         databasePinger
		wantHTTPStatus int
		want           readinessResponse
	}{
		{
			name:           "all databases ready",
			admin:          readyDatabase,
			target:         readyDatabase,
			wantHTTPStatus: http.StatusOK,
			want:           readinessResponse{Status: "ready", AdminDatabase: "ready", TargetDatabase: "ready"},
		},
		{
			name:           "target not configured",
			admin:          readyDatabase,
			target:         nil,
			wantHTTPStatus: http.StatusOK,
			want:           readinessResponse{Status: "ready", AdminDatabase: "ready", TargetDatabase: "not_configured"},
		},
		{
			name:           "admin unavailable",
			admin:          unavailableDatabase,
			target:         readyDatabase,
			wantHTTPStatus: http.StatusServiceUnavailable,
			want:           readinessResponse{Status: "not_ready", AdminDatabase: "unavailable", TargetDatabase: "ready"},
		},
		{
			name:           "target unavailable",
			admin:          readyDatabase,
			target:         unavailableDatabase,
			wantHTTPStatus: http.StatusServiceUnavailable,
			want:           readinessResponse{Status: "not_ready", AdminDatabase: "ready", TargetDatabase: "unavailable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()

			NewRouter(tt.admin, tt.target).ServeHTTP(response, request)

			if response.Code != tt.wantHTTPStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantHTTPStatus)
			}
			var got readinessResponse
			if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got != tt.want {
				t.Errorf("response = %+v, want %+v", got, tt.want)
			}
		})
	}
}

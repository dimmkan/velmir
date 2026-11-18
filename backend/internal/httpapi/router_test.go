package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"velmir/backend/internal/introspection"
)

type pingFunc func(context.Context) error

func (fn pingFunc) Ping(ctx context.Context) error {
	return fn(ctx)
}

type schemaInspectorFunc func(context.Context) (introspection.Catalog, error)

func (fn schemaInspectorFunc) Inspect(ctx context.Context) (introspection.Catalog, error) {
	return fn(ctx)
}

func TestHealthz(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(nil, nil, nil).ServeHTTP(response, request)

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

			NewRouter(tt.admin, tt.target, nil).ServeHTTP(response, request)

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

func TestSchema(t *testing.T) {
	catalog := introspection.Catalog{Schemas: []introspection.Schema{
		{
			Name: "public",
			Resources: []introspection.Resource{
				{Name: "books", Kind: "table", Columns: []introspection.Column{}, PrimaryKey: []string{"id"}, ForeignKeys: []introspection.ForeignKey{}},
			},
		},
	}}
	inspector := schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) {
		return catalog, nil
	})
	request := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	response := httptest.NewRecorder()

	NewRouter(nil, nil, inspector).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var got introspection.Catalog
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Schemas) != 1 || got.Schemas[0].Resources[0].Name != "books" {
		t.Errorf("response = %+v", got)
	}
}

func TestSchemaTargetNotConfigured(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	response := httptest.NewRecorder()

	NewRouter(nil, nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	var got errorResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Error.Code != "TARGET_NOT_CONFIGURED" {
		t.Errorf("error code = %q, want TARGET_NOT_CONFIGURED", got.Error.Code)
	}
}

func TestSchemaIntrospectionFailure(t *testing.T) {
	inspector := schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) {
		return introspection.Catalog{}, errors.New("read metadata")
	})
	request := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	response := httptest.NewRecorder()

	NewRouter(nil, nil, inspector).ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	var got errorResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Error.Code != "SCHEMA_INTROSPECTION_FAILED" {
		t.Errorf("error code = %q, want SCHEMA_INTROSPECTION_FAILED", got.Error.Code)
	}
}

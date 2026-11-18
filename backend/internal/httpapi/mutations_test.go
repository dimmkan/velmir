package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

type recordWriterFunc struct {
	create func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error)
	update func(context.Context, string, introspection.Resource, string, records.RecordValues) (map[string]any, error)
}

func (fn recordWriterFunc) Create(ctx context.Context, schema string, resource introspection.Resource, values records.RecordValues) (map[string]any, error) {
	return fn.create(ctx, schema, resource, values)
}

func (fn recordWriterFunc) Update(ctx context.Context, schema string, resource introspection.Resource, key string, values records.RecordValues) (map[string]any, error) {
	return fn.update(ctx, schema, resource, key, values)
}

func TestMutationsTargetReadOnly(t *testing.T) {
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{"name":"x"}}`)),
		httptest.NewRequest(http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, strings.NewReader(`{"values":{"name":"x"}}`)),
	} {
		response := httptest.NewRecorder()
		NewRouterWithWriter(nil, nil, recordsInspector(), nil, nil).ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("status = %d, body = %s", response.Code, response.Body.String())
		}
		var body errorResponse
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Code != "TARGET_READ_ONLY" {
			t.Errorf("code = %q", body.Error.Code)
		}
	}
}

func TestCreateAndUpdateRecords(t *testing.T) {
	var gotKey string
	writer := recordWriterFunc{
		create: func(_ context.Context, schema string, resource introspection.Resource, values records.RecordValues) (map[string]any, error) {
			if schema != "public" || resource.Name != "items" || string(values["name"]) != `"new"` {
				t.Errorf("create arguments = %s, %s, %#v", schema, resource.Name, values)
			}
			return map[string]any{"id": "9223372036854775807", "name": "new"}, nil
		},
		update: func(_ context.Context, _ string, _ introspection.Resource, key string, values records.RecordValues) (map[string]any, error) {
			gotKey = key
			if string(values["name"]) != `"updated"` {
				t.Errorf("values = %#v", values)
			}
			return map[string]any{"id": "1", "name": "updated"}, nil
		},
	}
	router := NewRouterWithWriter(nil, nil, recordsInspector(), nil, writer)

	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{"name":"new"}}`)))
	if createResponse.Code != http.StatusCreated || !strings.Contains(createResponse.Body.String(), `"id":"9223372036854775807"`) {
		t.Errorf("create response = %d %s", createResponse.Code, createResponse.Body.String())
	}

	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, httptest.NewRequest(http.MethodPatch, `/api/resources/public/items/records/%7B%22id%22%3A%221%22%7D`, strings.NewReader(`{"values":{"name":"updated"}}`)))
	if updateResponse.Code != http.StatusOK || gotKey != `{"id":"1"}` {
		t.Errorf("update response = %d %s, key = %q", updateResponse.Code, updateResponse.Body.String(), gotKey)
	}
}

func TestMutationValidationAndFieldErrors(t *testing.T) {
	writer := recordWriterFunc{
		create: func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error) {
			return nil, &records.MutationError{Code: "UNIQUE_VIOLATION", Message: "A record with these values already exists", FieldErrors: map[string]string{"name": "Already used"}}
		},
	}
	response := httptest.NewRecorder()
	NewRouterWithWriter(nil, nil, recordsInspector(), nil, writer).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{"name":"new"}}`)))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d", response.Code)
	}
	var body errorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "UNIQUE_VIOLATION" || body.Error.FieldErrors["name"] != "Already used" {
		t.Errorf("body = %+v", body)
	}

	invalid := httptest.NewRecorder()
	NewRouterWithWriter(nil, nil, recordsInspector(), nil, writer).ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{},"extra":true}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Errorf("invalid body status = %d", invalid.Code)
	}
}

func TestMutationFailureDoesNotLeakInternals(t *testing.T) {
	writer := recordWriterFunc{
		create: func(context.Context, string, introspection.Resource, records.RecordValues) (map[string]any, error) {
			return nil, errors.New("INSERT INTO secret SQL; stack trace")
		},
	}
	response := httptest.NewRecorder()
	NewRouterWithWriter(nil, nil, recordsInspector(), nil, writer).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/resources/public/items/records", strings.NewReader(`{"values":{}}`)))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret SQL") {
		t.Errorf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestSchemaReportsWriteEnabled(t *testing.T) {
	writer := recordWriterFunc{}
	response := httptest.NewRecorder()
	NewRouterWithWriter(nil, nil, recordsInspector(), nil, writer).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/schema", nil))
	var body introspection.Catalog
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.WriteEnabled {
		t.Error("write_enabled = false")
	}
}

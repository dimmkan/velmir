package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

type recordListerFunc func(context.Context, string, introspection.Resource, records.ListOptions) (records.ListResult, error)

func (fn recordListerFunc) List(ctx context.Context, schema string, resource introspection.Resource, options records.ListOptions) (records.ListResult, error) {
	return fn(ctx, schema, resource, options)
}

func recordsCatalog() introspection.Catalog {
	idDefault := "nextval('items_id_seq'::regclass)"
	return introspection.Catalog{Schemas: []introspection.Schema{{
		Name: "public",
		Resources: []introspection.Resource{{
			Name:       "items",
			Kind:       "table",
			PrimaryKey: []string{"id"},
			Columns: []introspection.Column{
				{Name: "id", DataType: "bigint", PrimaryKey: true, Default: &idDefault},
				{Name: "name", DataType: "text", Editable: true},
				{Name: "big", DataType: "bigint", Nullable: true},
			},
			ForeignKeys: []introspection.ForeignKey{},
		}},
	}}}
}

func recordsInspector() schemaInspector {
	return schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) {
		return recordsCatalog(), nil
	})
}

func TestListRecords(t *testing.T) {
	var gotOptions records.ListOptions
	lister := recordListerFunc(func(_ context.Context, schema string, resource introspection.Resource, options records.ListOptions) (records.ListResult, error) {
		if schema != "public" || resource.Name != "items" {
			t.Errorf("resource = %s.%s", schema, resource.Name)
		}
		gotOptions = options
		return records.ListResult{Records: []map[string]any{{"id": "9007199254740993"}}, Page: 2, Limit: 1, HasNext: true}, nil
	})
	request := httptest.NewRequest(http.MethodGet, "/api/resources/public/items/records?page=2&limit=1&sort=name&direction=desc&search=abc&search_column=name", nil)
	response := httptest.NewRecorder()

	newTestRouter(nil, nil, recordsInspector(), lister).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if gotOptions.Page != 2 || gotOptions.Limit != 1 || gotOptions.Sort != "name" || gotOptions.Direction != "desc" || !reflect.DeepEqual(gotOptions.SearchColumns, []string{"name"}) {
		t.Errorf("options = %+v", gotOptions)
	}
	var body records.ListResult
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Records) != 1 || body.Records[0]["id"] != "9007199254740993" || !body.HasNext {
		t.Errorf("body = %+v", body)
	}
}

func TestListRecordsValidation(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantCode   string
	}{
		{name: "unknown resource", target: "/api/resources/public/missing/records", wantStatus: http.StatusNotFound, wantCode: "RESOURCE_NOT_FOUND"},
		{name: "unknown schema", target: "/api/resources/missing/items/records", wantStatus: http.StatusNotFound, wantCode: "RESOURCE_NOT_FOUND"},
		{name: "bad page", target: "/api/resources/public/items/records?page=-1", wantStatus: http.StatusBadRequest, wantCode: "INVALID_PAGE"},
		{name: "bad limit", target: "/api/resources/public/items/records?limit=100000", wantStatus: http.StatusBadRequest, wantCode: "INVALID_LIMIT"},
		{name: "unknown column", target: "/api/resources/public/items/records?sort=missing", wantStatus: http.StatusBadRequest, wantCode: "INVALID_SORT"},
		{name: "sort injection", target: "/api/resources/public/items/records?sort=id%3BDROP%20TABLE%20items%3B--", wantStatus: http.StatusBadRequest, wantCode: "INVALID_SORT"},
		{name: "direction injection", target: "/api/resources/public/items/records?direction=asc%3BDROP%20TABLE%20items%3B--", wantStatus: http.StatusBadRequest, wantCode: "INVALID_DIRECTION"},
		{name: "filter injection", target: "/api/resources/public/items/records?filter=%5B%7B%22column%22%3A%22id%22%2C%22operator%22%3A%22eq%22%2C%22value%22%3A%221%20OR%20true%22%7D%5D", wantStatus: http.StatusBadRequest, wantCode: "INVALID_FILTER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			lister := recordListerFunc(func(context.Context, string, introspection.Resource, records.ListOptions) (records.ListResult, error) {
				called = true
				return records.ListResult{}, nil
			})
			request := httptest.NewRequest(http.MethodGet, tt.target, nil)
			response := httptest.NewRecorder()

			newTestRouter(nil, nil, recordsInspector(), lister).ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			var body errorResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", body.Error.Code, tt.wantCode)
			}
			if called {
				t.Error("lister was called for an invalid request")
			}
		})
	}
}

func TestListRecordsTargetNotConfigured(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/resources/public/items/records", nil)
	response := httptest.NewRecorder()
	newTestRouter(nil, nil, nil).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d", response.Code)
	}
}

func TestListRecordsQueryFailureIsGeneric(t *testing.T) {
	lister := recordListerFunc(func(context.Context, string, introspection.Resource, records.ListOptions) (records.ListResult, error) {
		return records.ListResult{}, errors.New("SQL SELECT secret; stack trace")
	})
	request := httptest.NewRequest(http.MethodGet, "/api/resources/public/items/records", nil)
	response := httptest.NewRecorder()
	newTestRouter(nil, nil, recordsInspector(), lister).ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
	var body errorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "RECORDS_QUERY_FAILED" || body.Error.Message != "Could not read resource records" {
		t.Errorf("body = %+v", body)
	}
}

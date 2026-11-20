package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"velmir/backend/internal/auth"
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

type relationResolverStub struct {
	plans  []records.RelationResolvePlan
	result records.RelationResolveResult
	err    error
	calls  int
}

func (s *relationResolverStub) List(context.Context, string, introspection.Resource, records.ListOptions) (records.ListResult, error) {
	return records.ListResult{}, errors.New("unexpected list call")
}

func (s *relationResolverStub) ResolveRelations(_ context.Context, plans []records.RelationResolvePlan) (records.RelationResolveResult, error) {
	s.calls++
	s.plans = plans
	if s.result.Relations == nil {
		s.result.Relations = make([]records.ResolvedRelation, 0)
	}
	return s.result, s.err
}

func relationHTTPInspector() schemaInspector {
	return schemaInspectorFunc(func(context.Context) (introspection.Catalog, error) {
		return introspection.Catalog{Schemas: []introspection.Schema{{Name: "public", Resources: []introspection.Resource{
			{
				Name: "books", Kind: "table",
				ForeignKeys: []introspection.ForeignKey{{
					Name: "books_author_fk", Columns: []string{"author_id"},
					ReferencedSchema: "public", ReferencedTable: "authors", ReferencedColumns: []string{"id"},
				}},
			},
			{
				Name: "authors", Kind: "table", PrimaryKey: []string{"id"},
				Columns: []introspection.Column{{Name: "id", DataType: "bigint"}, {Name: "name", DataType: "text"}},
			},
		}}}}, nil
	})
}

func TestResolveRelationsIsReadOnlyAndDoesNotRequireCSRF(t *testing.T) {
	resolver := &relationResolverStub{result: records.RelationResolveResult{Relations: []records.ResolvedRelation{{
		Name: "books_author_fk", Records: []map[string]any{{"id": "9223372036854775807", "name": "Author"}},
	}}}}
	router := NewRouter(nil, nil, relationHTTPInspector(), resolver, nil, testAuthService{user: testUser()}, testPermissionProvider{permissions: adminTestPermissions()}, nil, false)
	request := httptest.NewRequest(http.MethodPost, "/api/resources/public/books/relations/resolve", strings.NewReader(`{"relations":[{"name":"books_author_fk","keys":[{"id":"9223372036854775807"},{"id":"9223372036854775807"},{"id":null}]}]}`))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if resolver.calls != 1 || len(resolver.plans) != 1 || len(resolver.plans[0].Keys) != 1 {
		t.Fatalf("resolver calls/plans = %d/%+v", resolver.calls, resolver.plans)
	}
	if !strings.Contains(response.Body.String(), `"id":"9223372036854775807"`) {
		t.Errorf("response lost bigint precision: %s", response.Body.String())
	}
}

func TestResolveRelationsRequiresSessionAndCurrentRead(t *testing.T) {
	resolver := &relationResolverStub{}
	anonymousRouter := NewRouter(nil, nil, relationHTTPInspector(), resolver, nil, &fakeAuthService{authErr: auth.ErrUnauthorized}, testPermissionProvider{permissions: adminTestPermissions()}, nil, false)
	anonymous := httptest.NewRecorder()
	anonymousRouter.ServeHTTP(anonymous, httptest.NewRequest(http.MethodPost, "/api/resources/public/books/relations/resolve", strings.NewReader(`{"relations":[]}`)))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", anonymous.Code)
	}

	permissions := authorization.EffectivePermissions([]authorization.Grant{{Schema: "public", Resource: "authors", Operation: authorization.Read}})
	forbidden := httptest.NewRecorder()
	newRBACTestRouter(permissions, relationHTTPInspector(), resolver, nil).ServeHTTP(forbidden, httptest.NewRequest(http.MethodPost, "/api/resources/public/books/relations/resolve", strings.NewReader(`{"relations":[]}`)))
	if forbidden.Code != http.StatusForbidden || resolver.calls != 0 {
		t.Fatalf("current-resource permission = %d, calls = %d", forbidden.Code, resolver.calls)
	}
}

func TestResolveRelationsRejectsReferencedReadMissingAndUnknownRelation(t *testing.T) {
	resolver := &relationResolverStub{}
	permissions := authorization.EffectivePermissions([]authorization.Grant{{Schema: "public", Resource: "books", Operation: authorization.Read}})
	forbidden := httptest.NewRecorder()
	newRBACTestRouter(permissions, relationHTTPInspector(), resolver, nil).ServeHTTP(forbidden, httptest.NewRequest(http.MethodPost, "/api/resources/public/books/relations/resolve", strings.NewReader(`{"relations":[{"name":"books_author_fk","keys":[]}]}`)))
	if forbidden.Code != http.StatusForbidden || resolver.calls != 0 || strings.Contains(forbidden.Body.String(), "authors") {
		t.Fatalf("referenced-resource permission = %d %s, calls = %d", forbidden.Code, forbidden.Body.String(), resolver.calls)
	}

	unknown := httptest.NewRecorder()
	newRBACTestRouter(adminTestPermissions(), relationHTTPInspector(), resolver, nil).ServeHTTP(unknown, httptest.NewRequest(http.MethodPost, "/api/resources/public/books/relations/resolve", strings.NewReader(`{"relations":[{"name":"unknown","keys":[]}]}`)))
	if unknown.Code != http.StatusBadRequest || !strings.Contains(unknown.Body.String(), "INVALID_RELATION_RESOLVE") || resolver.calls != 0 {
		t.Fatalf("unknown relation = %d %s, calls = %d", unknown.Code, unknown.Body.String(), resolver.calls)
	}
}

func TestResolveRelationsRejectsMalformedBodyAndHidesQueryErrors(t *testing.T) {
	resolver := &relationResolverStub{}
	malformed := httptest.NewRecorder()
	newRBACTestRouter(adminTestPermissions(), relationHTTPInspector(), resolver, nil).ServeHTTP(malformed, httptest.NewRequest(http.MethodPost, "/api/resources/public/books/relations/resolve", strings.NewReader(`{"relations":[],"sql":"DROP TABLE books"}`)))
	if malformed.Code != http.StatusBadRequest || resolver.calls != 0 {
		t.Fatalf("malformed body = %d, calls = %d", malformed.Code, resolver.calls)
	}

	resolver.err = errors.New("SELECT secret; stack trace")
	failed := httptest.NewRecorder()
	newRBACTestRouter(adminTestPermissions(), relationHTTPInspector(), resolver, nil).ServeHTTP(failed, httptest.NewRequest(http.MethodPost, "/api/resources/public/books/relations/resolve", strings.NewReader(`{"relations":[]}`)))
	if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), "SELECT") || strings.Contains(failed.Body.String(), "stack") {
		t.Fatalf("query error leaked internals = %d %s", failed.Code, failed.Body.String())
	}
}

func TestReverseRelationsFollowReadableResourcesOnly(t *testing.T) {
	catalog := introspection.Catalog{Schemas: []introspection.Schema{
		{Name: "public", Resources: []introspection.Resource{
			{Name: "authors"},
			{Name: "books", ForeignKeys: []introspection.ForeignKey{{
				Name: "books_author_fk", Columns: []string{"author_id"},
				ReferencedSchema: "public", ReferencedTable: "authors", ReferencedColumns: []string{"id"},
			}}},
			{Name: "secret_children", ForeignKeys: []introspection.ForeignKey{{
				Name: "secret_parent_fk", Columns: []string{"parent_id"},
				ReferencedSchema: "public", ReferencedTable: "authors", ReferencedColumns: []string{"id"},
			}}},
		}},
		{Name: "private", Resources: []introspection.Resource{{Name: "hidden_parents"}}},
	}}
	permissions := authorization.EffectivePermissions([]authorization.Grant{
		{Schema: "public", Resource: "authors", Operation: authorization.Read},
		{Schema: "public", Resource: "books", Operation: authorization.Read},
	})
	filtered := filterReadableCatalog(catalog, permissions)
	authors, ok := filtered.FindResource("public", "authors")
	if !ok || len(authors.ReverseRelations) != 1 {
		t.Fatalf("authors reverse relations = %+v", authors.ReverseRelations)
	}
	relation := authors.ReverseRelations[0]
	if relation.Name != "books_author_fk" || relation.Schema != "public" || relation.Resource != "books" || len(relation.Columns) != 1 || relation.Columns[0] != "author_id" || relation.ReferencedColumns[0] != "id" {
		t.Errorf("reverse relation = %+v", relation)
	}
	encoded, err := json.Marshal(filtered)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret_children") || strings.Contains(string(encoded), "hidden_parents") {
		t.Errorf("filtered metadata leaks inaccessible resources: %s", encoded)
	}
}

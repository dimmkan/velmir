//go:build integration

package records

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"velmir/backend/internal/introspection"
)

func TestLocalDatabaseResolveSimpleAndCompositeRelations(t *testing.T) {
	databaseURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_TARGET_DATABASE_URL is not set")
	}
	assertLocalRelationDatabase(t, databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	catalog, err := introspection.New(database).Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	books, ok := catalog.FindResource("public", "books")
	if !ok {
		t.Fatal("public.books is missing")
	}
	request := RelationResolveRequest{Relations: []RelationResolveRelation{{
		Name: "books_author_id_fkey",
		Keys: []map[string]json.RawMessage{
			rawKey(t, map[string]any{"id": "1"}),
			rawKey(t, map[string]any{"id": 1}),
			rawKey(t, map[string]any{"id": nil}),
		},
	}}}
	plans, err := PrepareRelationResolve(request, books, "public", catalog, allowAllRelations, allowAllRelationFields)
	if err != nil {
		t.Fatal(err)
	}
	result, err := New(database).ResolveRelations(ctx, plans)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Relations) != 1 || len(result.Relations[0].Records) != 1 {
		t.Fatalf("simple relation result = %+v", result)
	}
	record := result.Relations[0].Records[0]
	if id, ok := record["id"].(string); !ok || id != "1" {
		t.Errorf("bigint id = %#v (%T), want exact string", record["id"], record["id"])
	}
	if _, ok := record["name"].(string); !ok {
		t.Errorf("display label name = %#v", record["name"])
	}
	if _, exposed := record["created_at"]; exposed {
		t.Errorf("batch selected a field not needed for the key or label: %+v", record)
	}

	rows, err := database.Query(ctx, `SELECT id, author_id FROM public.books ORDER BY id LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("books has no local demonstration row")
	}
	var bookID, authorID int64
	if err := rows.Scan(&bookID, &authorID); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	composite := RelationResolvePlan{
		Name: "composite_book_key", Schema: "public", Resource: books,
		ForeignKey: introspection.ForeignKey{ReferencedColumns: []string{"id", "author_id"}},
		Keys:       []RelationResolveKey{{Values: []any{bookID, authorID}}},
	}
	compositeResult, err := New(database).ResolveRelations(ctx, []RelationResolvePlan{composite})
	if err != nil {
		t.Fatal(err)
	}
	if len(compositeResult.Relations[0].Records) != 1 {
		t.Fatalf("composite relation result = %+v", compositeResult)
	}
	compositeRecord := compositeResult.Relations[0].Records[0]
	if compositeRecord["id"] != strconv.FormatInt(bookID, 10) || compositeRecord["author_id"] != strconv.FormatInt(authorID, 10) {
		t.Errorf("composite key precision = %+v", compositeRecord)
	}
}

func assertLocalRelationDatabase(t *testing.T, raw string) {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	host := parsed.Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		t.Fatalf("refusing non-local relation integration database host %q", host)
	}
	if parsed.Path != "/velmir_demo" {
		t.Fatalf("refusing relation integration database %q", parsed.Path)
	}
}

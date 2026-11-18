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

func TestServicePaginationSortingAndFilters(t *testing.T) {
	databaseURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_TARGET_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	database, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create target pool: %v", err)
	}
	defer database.Close()
	if err := database.Ping(ctx); err != nil {
		t.Fatalf("ping target database: %v", err)
	}

	catalog, err := introspection.New(database).Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect target database: %v", err)
	}
	books, ok := catalog.FindResource("public", "books")
	if !ok {
		t.Fatal("public.books is missing")
	}
	service := New(database)

	firstPage := listForTest(t, ctx, service, books, url.Values{
		"page": {"1"}, "limit": {"2"}, "sort": {"id"}, "direction": {"asc"},
	})
	secondPage := listForTest(t, ctx, service, books, url.Values{
		"page": {"2"}, "limit": {"2"}, "sort": {"id"}, "direction": {"asc"},
	})
	if len(firstPage.Records) != 2 || !firstPage.HasNext {
		t.Fatalf("first page = %+v, want two records and has_next", firstPage)
	}
	if len(secondPage.Records) != 2 || secondPage.HasNext {
		t.Fatalf("second page = %+v, want two records and no next page", secondPage)
	}
	ids := make([]int64, 0, 4)
	for _, page := range []ListResult{firstPage, secondPage} {
		for _, record := range page.Records {
			id, ok := record["id"].(string)
			if !ok {
				t.Fatalf("bigint id has type %T, want string", record["id"])
			}
			parsed, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				t.Fatalf("parse bigint id: %v", err)
			}
			ids = append(ids, parsed)
			if _, ok := record["metadata"].(json.RawMessage); !ok {
				t.Errorf("jsonb metadata has type %T, want json.RawMessage", record["metadata"])
			}
		}
	}
	for index := 1; index < len(ids); index++ {
		if ids[index-1] >= ids[index] {
			t.Errorf("ids are not sorted and unique across pages: %v", ids)
			break
		}
	}

	descending := listForTest(t, ctx, service, books, url.Values{
		"limit": {"4"}, "sort": {"title"}, "direction": {"desc"},
	})
	for index := 1; index < len(descending.Records); index++ {
		if descending.Records[index-1]["title"].(string) < descending.Records[index]["title"].(string) {
			t.Errorf("titles are not descending: %+v", descending.Records)
			break
		}
	}

	filtered := listForTest(t, ctx, service, books, url.Values{
		"filter": {`[
			{"column":"title","operator":"contains","value":"PostgreSQL"},
			{"column":"published_on","operator":"gte","value":"2024-02-01"},
			{"column":"isbn","operator":"is_null","value":false}
		]`},
	})
	if len(filtered.Records) != 2 {
		t.Fatalf("filtered records = %+v, want two PostgreSQL books with ISBNs", filtered)
	}
	for _, record := range filtered.Records {
		title := record["title"].(string)
		if title != "PostgreSQL Guide" && title != "PostgreSQL Cookbook" {
			t.Errorf("unexpected filtered title %q", title)
		}
	}

	searched := listForTest(t, ctx, service, books, url.Values{
		"search": {"Advanced"}, "search_column": {"title"},
	})
	if len(searched.Records) != 1 || searched.Records[0]["title"] != "Advanced Queries" {
		t.Errorf("search result = %+v", searched)
	}

	nullOnly := listForTest(t, ctx, service, books, url.Values{
		"filter": {`[{"column":"published_on","operator":"is_null","value":true}]`},
	})
	if len(nullOnly.Records) != 1 || nullOnly.Records[0]["published_on"] != nil {
		t.Errorf("null filter result = %+v", nullOnly)
	}
}

func listForTest(t *testing.T, ctx context.Context, service *Service, resource introspection.Resource, query url.Values) ListResult {
	t.Helper()
	options, err := ParseListOptions(query, resource)
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	result, err := service.List(ctx, "public", resource, options)
	if err != nil {
		t.Fatalf("list records with %v: %v", query, err)
	}
	return result
}

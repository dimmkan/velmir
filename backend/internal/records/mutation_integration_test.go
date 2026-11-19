//go:build integration

package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"velmir/backend/internal/introspection"
)

func TestWriterDemoDatabase(t *testing.T) {
	readURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	writeURL := os.Getenv("TEST_TARGET_WRITE_DATABASE_URL")
	if readURL == "" || writeURL == "" {
		t.Skip("TEST_TARGET_DATABASE_URL and TEST_TARGET_WRITE_DATABASE_URL are not set")
	}
	assertLocalDemoURL(t, readURL)
	assertLocalDemoURL(t, writeURL)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	readDatabase, err := pgxpool.New(ctx, readURL)
	if err != nil {
		t.Fatal(err)
	}
	defer readDatabase.Close()
	writeDatabase, err := pgxpool.New(ctx, writeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer writeDatabase.Close()
	if err := readDatabase.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := writeDatabase.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	catalog, err := introspection.New(readDatabase).Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reader := New(readDatabase)
	writer := NewWriter(writeDatabase)
	suffix := fmt.Sprintf("integration-%d", time.Now().UnixNano())

	authors, ok := catalog.FindResource("public", "authors")
	if !ok {
		t.Fatal("authors resource is missing")
	}
	authorPage, err := reader.List(ctx, "public", authors, ListOptions{Page: 1, Limit: 1, Direction: "asc", Filters: []Filter{}, SearchColumns: []string{}})
	if err != nil || len(authorPage.Records) != 1 {
		t.Fatalf("list authors: %v, %#v", err, authorPage)
	}
	authorID := authorPage.Records[0]["id"].(string)

	for _, blankName := range []string{"", " \t\n "} {
		_, err = writer.Create(ctx, "public", authors, testValues(t, map[string]any{"name": blankName}))
		var validationErr *MutationError
		if !errors.As(err, &validationErr) || validationErr.Code != "INVALID_RECORD" || validationErr.FieldErrors["name"] == "" {
			t.Errorf("blank author name error = %#v, want field validation error", err)
		}
	}

	categories, _ := catalog.FindResource("public", "categories")
	category, err := writer.Create(ctx, "public", categories, testValues(t, map[string]any{"name": suffix}))
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	categoryID := category["id"].(string)

	books, _ := catalog.FindResource("public", "books")
	book, err := writer.Create(ctx, "public", books, testValues(t, map[string]any{
		"author_id": authorID,
		"title":     suffix,
		"metadata":  map[string]any{"source": "integration"},
	}))
	if err != nil {
		t.Fatalf("create book: %v", err)
	}
	bookID := book["id"].(string)
	if book["title"] != suffix || book["metadata"] == nil {
		t.Errorf("returned book = %#v", book)
	}

	updatedBook, err := writer.Update(ctx, "public", books, testKey(t, map[string]any{"id": bookID}), testValues(t, map[string]any{"title": suffix + "-updated", "published_on": nil}))
	if err != nil {
		t.Fatalf("update book: %v", err)
	}
	if updatedBook.After["title"] != suffix+"-updated" || updatedBook.After["published_on"] != nil || updatedBook.Before["title"] != suffix {
		t.Errorf("updated book = %#v, before = %#v", updatedBook.After, updatedBook.Before)
	}

	links, _ := catalog.FindResource("public", "book_categories")
	key := map[string]any{"book_id": bookID, "category_id": categoryID}
	_, err = writer.Create(ctx, "public", links, testValues(t, map[string]any{"book_id": bookID, "category_id": categoryID, "note": "before"}))
	if err != nil {
		t.Fatalf("create composite-key record: %v", err)
	}
	updatedLink, err := writer.Update(ctx, "public", links, testKey(t, key), testValues(t, map[string]any{"note": "after"}))
	if err != nil {
		t.Fatalf("update composite-key record: %v", err)
	}
	if updatedLink.After["note"] != "after" || updatedLink.After["book_id"] != bookID || updatedLink.After["category_id"] != categoryID || updatedLink.Before["note"] != "before" {
		t.Errorf("updated composite-key record = %#v, before = %#v", updatedLink.After, updatedLink.Before)
	}
}

func assertLocalDemoURL(t *testing.T, raw string) {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	host := parsed.Hostname()
	if (host != "localhost" && host != "127.0.0.1" && host != "::1") || parsed.Path != "/velmir_demo" {
		t.Fatalf("integration writes are allowed only against the local velmir_demo database")
	}
}

func testValues(t *testing.T, values map[string]any) RecordValues {
	t.Helper()
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var result RecordValues
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func testKey(t *testing.T, key map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

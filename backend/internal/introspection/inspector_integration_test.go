//go:build integration

package introspection

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInspectorDemoDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_TARGET_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	database, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create target pool: %v", err)
	}
	defer database.Close()
	if err := database.Ping(ctx); err != nil {
		t.Fatalf("ping target database: %v", err)
	}

	catalog, err := New(database).Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect target database: %v", err)
	}

	books := findResource(catalog, "public", "books")
	if books == nil {
		t.Fatal("public.books is missing")
	}
	if len(books.PrimaryKey) != 1 || books.PrimaryKey[0] != "id" {
		t.Errorf("books primary key = %v", books.PrimaryKey)
	}
	if len(books.ForeignKeys) == 0 {
		t.Error("books foreign keys are missing")
	}
	if view := findResource(catalog, "public", "books_overview"); view == nil || view.Kind != "view" {
		t.Error("public.books_overview view is missing")
	}
}

func findResource(catalog Catalog, schema, resource string) *Resource {
	for _, schemaMetadata := range catalog.Schemas {
		if schemaMetadata.Name != schema {
			continue
		}
		for i := range schemaMetadata.Resources {
			if schemaMetadata.Resources[i].Name == resource {
				return &schemaMetadata.Resources[i]
			}
		}
	}
	return nil
}

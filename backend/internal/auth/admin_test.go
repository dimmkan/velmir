package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type createUserRow struct{}

func (createUserRow) Scan(destinations ...any) error {
	*(destinations[0].(*int64)) = 42
	return nil
}

type createUserDB struct {
	query string
	args  []any
}

func (db *createUserDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	db.query = query
	db.args = args
	return createUserRow{}
}

func (db *createUserDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func TestCreateUserStoresOnlyArgon2idHash(t *testing.T) {
	db := &createUserDB{}
	user, err := CreateUser(context.Background(), db, " Viewer@Example.Test ", "Viewer", "a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != 42 || user.Email != "viewer@example.test" || !user.IsActive {
		t.Fatalf("user = %+v", user)
	}
	if len(db.args) != 3 || db.args[0] != user.Email || db.args[2] != "Viewer" {
		t.Fatalf("query arguments = %#v", db.args)
	}
	hash, ok := db.args[1].(string)
	if !ok || !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, "a-long-test-password") || strings.Contains(db.query, "a-long-test-password") {
		t.Fatalf("password was not safely hashed and parameterized")
	}
}

func TestCreateUserRejectsInvalidCredentialsBeforeDatabase(t *testing.T) {
	for _, test := range []struct {
		email    string
		password string
	}{
		{email: "invalid login", password: "a-long-test-password"},
		{email: "viewer@example.test", password: "short"},
		{email: "viewer@example.test", password: ""},
	} {
		if _, err := CreateUser(context.Background(), &createUserDB{}, test.email, "", test.password); err == nil {
			t.Fatalf("CreateUser(%q, %q) succeeded", test.email, test.password)
		}
	}
}

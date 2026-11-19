//go:build integration

package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLocalAdminDatabaseAuditStorage(t *testing.T) {
	databaseURL := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_ADMIN_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Path != "/velmir_admin" || !localAuditHost(parsed.Hostname()) {
		t.Fatal("TEST_ADMIN_DATABASE_URL must point to the local velmir_admin database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var actorID int64
	if err := tx.QueryRow(ctx, `INSERT INTO users (email, password_hash, display_name) VALUES ('audit-integration@example.test', 'not-a-real-password-hash', 'Audit Actor') RETURNING id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	store := NewStore(tx)
	key := json.RawMessage(`{"tenant_id":"7","id":"9223372036854775807"}`)
	before := map[string]any{"amount": "1.00", "payload": json.RawMessage(`{"big":9223372036854775806}`), "data": "AAH/", "active": false}
	after := map[string]any{"amount": "12345678901234567890.0001", "payload": json.RawMessage(`{"big":9223372036854775807,"decimal":12345678901234567890.0001}`), "data": "AAH/", "active": true}
	if err := store.Record(ctx, Input{ActorUserID: actorID, Action: Create, Schema: "public", Resource: "items", RecordKey: key, After: before}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := store.Record(ctx, Input{ActorUserID: actorID, Action: Update, Schema: "public", Resource: "items", RecordKey: key, Before: before, After: after}); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(ctx, Input{ActorUserID: actorID, Action: Create, Schema: "private", Resource: "items", RecordKey: key, After: after}); err != nil {
		t.Fatal(err)
	}

	page, err := store.History(ctx, "public", "items", key, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || !page.HasNext || page.Events[0].Action != Update || page.Events[0].Actor.ID != actorID || page.Events[0].Timestamp.IsZero() {
		t.Fatalf("first history page = %+v", page)
	}
	event := page.Events[0]
	var afterDocument struct {
		Amount  string                 `json:"amount"`
		Data    string                 `json:"data"`
		Active  bool                   `json:"active"`
		Payload map[string]json.Number `json:"payload"`
	}
	decoder := json.NewDecoder(bytes.NewReader(event.After))
	decoder.UseNumber()
	if err := decoder.Decode(&afterDocument); err != nil {
		t.Fatal(err)
	}
	if afterDocument.Amount != "12345678901234567890.0001" || afterDocument.Data != "AAH/" || !afterDocument.Active || afterDocument.Payload["big"].String() != "9223372036854775807" || afterDocument.Payload["decimal"].String() != "12345678901234567890.0001" {
		t.Errorf("exact after representation = %s", event.After)
	}
	var beforeDocument struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(event.Before, &beforeDocument); err != nil || beforeDocument.Active {
		t.Errorf("before representation = %s, error = %v", event.Before, err)
	}
	second, err := store.History(ctx, "public", "items", key, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 1 || second.HasNext || second.Events[0].Action != Create || second.Events[0].Before != nil {
		t.Fatalf("second history page = %+v", second)
	}
	if page.Events[0].Timestamp.Before(second.Events[0].Timestamp) {
		t.Fatal("history is not in reverse chronological order")
	}
	empty, err := store.History(ctx, "public", "items", json.RawMessage(`{"id":"1"}`), 1, 20)
	if err != nil || len(empty.Events) != 0 || empty.HasNext {
		t.Fatalf("empty history = %+v, %v", empty, err)
	}

	deleteKey := json.RawMessage(`{"id":"delete-integration"}`)
	if err := store.Record(ctx, Input{ActorUserID: actorID, Action: Delete, Schema: "public", Resource: "items", RecordKey: deleteKey, Before: before}); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.History(ctx, "public", "items", deleteKey, 1, 20)
	if err != nil || len(deleted.Events) != 1 {
		t.Fatalf("delete history = %+v, %v", deleted, err)
	}
	event = deleted.Events[0]
	if event.Action != Delete || len(event.After) != 0 {
		t.Errorf("delete action/after = %q / %s", event.Action, event.After)
	}
	if !bytes.Contains(event.Before, []byte(`"amount": "1.00"`)) {
		t.Errorf("delete before = %s", event.Before)
	}
}

func localAuditHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

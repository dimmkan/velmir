package audit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"velmir/backend/internal/introspection"
)

func TestRecordKeySupportsCompositePrimaryKeys(t *testing.T) {
	resource := introspection.Resource{PrimaryKey: []string{"tenant_id", "id"}}
	key, err := RecordKey(resource, map[string]any{"id": "9223372036854775807", "tenant_id": "7", "name": "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if string(key) != `{"id":"9223372036854775807","tenant_id":"7"}` {
		t.Fatalf("key = %s", key)
	}
	if _, err := RecordKey(resource, map[string]any{"id": "1"}); err == nil {
		t.Fatal("missing composite key part was accepted")
	}
}

func TestMarshalRecordOmitsSecretsAndPreservesExactValues(t *testing.T) {
	value, err := marshalRecord(map[string]any{
		"id":                "9223372036854775807",
		"amount":            "12345678901234567890.0001",
		"payload":           json.RawMessage(`{"big":9223372036854775807,"decimal":12345678901234567890.0001,"password":"nested-secret","items":[{"csrf_token":"nested-token","ok":true}]}`),
		"data":              "AAH/",
		"user_password":     "plain-secret",
		"password_hash":     "hash-secret",
		"session_token":     "session-secret",
		"csrf_token":        "csrf-secret",
		"admin_api_secret":  "admin-secret",
		"connection_string": "connection-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"plain-secret", "hash-secret", "session-secret", "csrf-secret", "admin-secret", "connection-secret", "nested-secret", "nested-token"} {
		if strings.Contains(text, secret) {
			t.Errorf("audit payload contains %q: %s", secret, text)
		}
	}
	for _, exact := range []string{`"id":"9223372036854775807"`, `"amount":"12345678901234567890.0001"`, `"big":9223372036854775807`, `"decimal":12345678901234567890.0001`, `"data":"AAH/"`} {
		if !strings.Contains(text, exact) {
			t.Errorf("audit payload does not preserve %s: %s", exact, text)
		}
	}

	var decoded map[string]any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	payload := decoded["payload"].(map[string]any)
	if payload["big"].(json.Number).String() != "9223372036854775807" || payload["decimal"].(json.Number).String() != "12345678901234567890.0001" {
		t.Errorf("JSON numbers lost precision: %#v", payload)
	}
	wantItems := []any{map[string]any{"ok": true}}
	if !reflect.DeepEqual(payload["items"], wantItems) {
		t.Errorf("sanitized nested items = %#v", payload["items"])
	}
}

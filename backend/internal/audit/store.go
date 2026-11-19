package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"velmir/backend/internal/introspection"
)

type Action string

const (
	Create Action = "create"
	Update Action = "update"
	Delete Action = "delete"
)

type Actor struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type Input struct {
	ActorUserID int64
	Action      Action
	Schema      string
	Resource    string
	RecordKey   json.RawMessage
	Before      map[string]any
	After       map[string]any
}

type Event struct {
	ID        string          `json:"id"`
	Actor     Actor           `json:"actor"`
	Timestamp time.Time       `json:"timestamp"`
	Action    Action          `json:"action"`
	Schema    string          `json:"schema"`
	Resource  string          `json:"resource"`
	RecordKey json.RawMessage `json:"record_key"`
	Before    json.RawMessage `json:"before"`
	After     json.RawMessage `json:"after"`
}

type HistoryResult struct {
	Events  []Event `json:"events"`
	Page    int64   `json:"page"`
	Limit   int     `json:"limit"`
	HasNext bool    `json:"has_next"`
}

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type Store struct {
	database DBTX
}

func NewStore(database DBTX) *Store {
	return &Store{database: database}
}

func RecordKey(resource introspection.Resource, record map[string]any) (json.RawMessage, error) {
	key := make(map[string]any, len(resource.PrimaryKey))
	for _, name := range resource.PrimaryKey {
		value, ok := record[name]
		if !ok || value == nil {
			return nil, fmt.Errorf("returned record is missing primary key column %q", name)
		}
		key[name] = value
	}
	encoded, err := json.Marshal(key)
	if err != nil {
		return nil, fmt.Errorf("encode record key: %w", err)
	}
	return encoded, nil
}

func (s *Store) Record(ctx context.Context, input Input) error {
	if input.ActorUserID <= 0 || input.Schema == "" || input.Resource == "" || !json.Valid(input.RecordKey) {
		return errors.New("invalid audit event identity")
	}
	if input.Action != Create && input.Action != Update && input.Action != Delete {
		return errors.New("invalid audit event action")
	}
	if input.Action == Create && (input.Before != nil || input.After == nil) {
		return errors.New("create audit event must contain only after data")
	}
	if input.Action == Update && (input.Before == nil || input.After == nil) {
		return errors.New("update audit event must contain before and after data")
	}
	if input.Action == Delete && (input.Before == nil || input.After != nil) {
		return errors.New("delete audit event must contain only before data")
	}

	before, err := marshalRecord(input.Before)
	if err != nil {
		return fmt.Errorf("encode audit before data: %w", err)
	}
	after, err := marshalRecord(input.After)
	if err != nil {
		return fmt.Errorf("encode audit after data: %w", err)
	}
	_, err = s.database.Exec(ctx, `
		INSERT INTO audit_events (
			actor_user_id, action, schema_name, resource_name, record_key, before_data, after_data
		) VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb)`,
		input.ActorUserID, string(input.Action), input.Schema, input.Resource,
		json.RawMessage(input.RecordKey), before, after)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func (s *Store) History(ctx context.Context, schemaName, resourceName string, recordKey json.RawMessage, page int64, limit int) (HistoryResult, error) {
	result := HistoryResult{Events: make([]Event, 0, limit), Page: page, Limit: limit}
	rows, err := s.database.Query(ctx, `
		SELECT e.id, e.occurred_at, e.action, e.record_key, e.before_data, e.after_data,
		       u.id, u.email, u.display_name
		FROM audit_events AS e
		JOIN users AS u ON u.id = e.actor_user_id
		WHERE e.schema_name = $1
		  AND e.resource_name = $2
		  AND e.record_key @> $3::jsonb
		  AND e.record_key <@ $3::jsonb
		ORDER BY e.occurred_at DESC, e.id DESC
		LIMIT $4 OFFSET $5`, schemaName, resourceName, recordKey, limit+1, (page-1)*int64(limit))
	if err != nil {
		return HistoryResult{}, fmt.Errorf("query audit history: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var event Event
		var id pgtype.UUID
		var key, before, after []byte
		if err := rows.Scan(&id, &event.Timestamp, &event.Action, &key, &before, &after, &event.Actor.ID, &event.Actor.Email, &event.Actor.DisplayName); err != nil {
			return HistoryResult{}, fmt.Errorf("read audit history: %w", err)
		}
		if !id.Valid {
			return HistoryResult{}, errors.New("audit event has no ID")
		}
		event.ID = id.String()
		event.Schema = schemaName
		event.Resource = resourceName
		event.RecordKey = json.RawMessage(key)
		event.Before = json.RawMessage(before)
		event.After = json.RawMessage(after)
		if len(result.Events) == limit {
			result.HasNext = true
			continue
		}
		result.Events = append(result.Events, event)
	}
	if err := rows.Err(); err != nil {
		return HistoryResult{}, fmt.Errorf("read audit history: %w", err)
	}
	return result, nil
}

func marshalRecord(record map[string]any) (any, error) {
	if record == nil {
		return nil, nil
	}
	sanitized := make(map[string]any, len(record))
	for name, value := range record {
		if sensitiveField(name) {
			continue
		}
		if raw, ok := value.(json.RawMessage); ok {
			sanitizedJSON, err := sanitizeJSON(raw)
			if err != nil {
				return nil, fmt.Errorf("sanitize JSON column %q: %w", name, err)
			}
			value = sanitizedJSON
		}
		sanitized[name] = value
	}
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func sanitizeJSON(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON contains multiple values")
	}
	return sanitizeJSONValue(value), nil
}

func sanitizeJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for name, child := range typed {
			if !sensitiveField(name) {
				result[name] = sanitizeJSONValue(child)
			}
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = sanitizeJSONValue(child)
		}
		return result
	default:
		return value
	}
}

func sensitiveField(name string) bool {
	var normalized strings.Builder
	for _, character := range strings.ToLower(name) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			normalized.WriteRune(character)
		}
	}
	value := normalized.String()
	return strings.Contains(value, "password") ||
		strings.Contains(value, "passwd") ||
		value == "pwd" || strings.HasSuffix(value, "pwd") ||
		strings.Contains(value, "sessiontoken") ||
		strings.Contains(value, "csrftoken") ||
		strings.Contains(value, "secret") ||
		strings.Contains(value, "connectionstring") ||
		strings.Contains(value, "databaseurl") || value == "dsn"
}

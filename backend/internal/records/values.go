package records

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func jsonValue(value any, oid uint32) (any, error) {
	if value == nil {
		return nil, nil
	}

	switch oid {
	case pgtype.Int8OID:
		switch number := value.(type) {
		case int64:
			return fmt.Sprintf("%d", number), nil
		case pgtype.Int8:
			if !number.Valid {
				return nil, nil
			}
			return fmt.Sprintf("%d", number.Int64), nil
		}
	case pgtype.NumericOID:
		switch number := value.(type) {
		case pgtype.Numeric:
			return numericString(number)
		case *pgtype.Numeric:
			if number == nil {
				return nil, nil
			}
			return numericString(*number)
		}
	case pgtype.DateOID:
		if date, ok := value.(time.Time); ok {
			return date.Format("2006-01-02"), nil
		}
	case pgtype.TimestampOID:
		if timestamp, ok := value.(time.Time); ok {
			return timestamp.Format("2006-01-02T15:04:05.999999999"), nil
		}
	case pgtype.TimestamptzOID:
		if timestamp, ok := value.(time.Time); ok {
			return timestamp.Format(time.RFC3339Nano), nil
		}
	case pgtype.UUIDOID:
		switch id := value.(type) {
		case [16]byte:
			return formatUUID(id), nil
		case pgtype.UUID:
			if !id.Valid {
				return nil, nil
			}
			return id.String(), nil
		case string:
			return id, nil
		}
	case pgtype.JSONOID, pgtype.JSONBOID:
		switch document := value.(type) {
		case []byte:
			if document == nil {
				return nil, nil
			}
			if !json.Valid(document) {
				return nil, errors.New("database returned invalid JSON")
			}
			return json.RawMessage(append([]byte(nil), document...)), nil
		case string:
			if !json.Valid([]byte(document)) {
				return nil, errors.New("database returned invalid JSON")
			}
			return json.RawMessage(document), nil
		case json.RawMessage:
			if !json.Valid(document) {
				return nil, errors.New("database returned invalid JSON")
			}
			return document, nil
		}
	case pgtype.ByteaOID:
		if bytes, ok := value.([]byte); ok {
			return base64.StdEncoding.EncodeToString(bytes), nil
		}
	}

	switch typed := value.(type) {
	case string, bool, int16, int32:
		return typed, nil
	case float32:
		number := float64(typed)
		if math.IsInf(number, 0) || math.IsNaN(number) {
			return fmt.Sprint(typed), nil
		}
		return typed, nil
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return fmt.Sprint(typed), nil
		}
		return typed, nil
	case int:
		return typed, nil
	case int64:
		return fmt.Sprintf("%d", typed), nil
	case uint32:
		return typed, nil
	case time.Time:
		return typed.Format(time.RFC3339Nano), nil
	case []byte:
		return base64.StdEncoding.EncodeToString(typed), nil
	default:
		return fmt.Sprint(value), nil
	}
}

func numericString(number pgtype.Numeric) (any, error) {
	if !number.Valid {
		return nil, nil
	}
	value, err := number.Value()
	if err != nil {
		return nil, err
	}
	text, ok := value.(string)
	if !ok {
		return nil, errors.New("could not represent numeric as text")
	}
	return text, nil
}

func formatUUID(id [16]byte) string {
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], id[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], id[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], id[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], id[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], id[10:16])
	return string(encoded)
}

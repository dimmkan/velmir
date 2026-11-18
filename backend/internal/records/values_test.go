package records

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestJSONValuePreservesLargeNumbers(t *testing.T) {
	bigint, err := jsonValue(int64(9223372036854775807), pgtype.Int8OID)
	if err != nil {
		t.Fatal(err)
	}
	if bigint != "9223372036854775807" {
		t.Errorf("bigint = %#v", bigint)
	}

	numeric := pgtype.Numeric{Int: new(big.Int), Exp: -18, Valid: true}
	numeric.Int.SetString("123456789012345678900123456789", 10)
	value, err := jsonValue(numeric, pgtype.NumericOID)
	if err != nil {
		t.Fatal(err)
	}
	if value != "123456789012.345678900123456789" {
		t.Errorf("numeric = %#v", value)
	}
}

func TestJSONValueFormatsKnownTypes(t *testing.T) {
	timestamp := time.Date(2025, time.January, 2, 3, 4, 5, 123456789, time.FixedZone("UTC+3", 3*60*60))
	uuid := [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	tests := []struct {
		name  string
		value any
		oid   uint32
		want  any
	}{
		{name: "null", value: nil, oid: pgtype.TextOID, want: nil},
		{name: "null JSON bytes", value: []byte(nil), oid: pgtype.JSONBOID, want: nil},
		{name: "boolean", value: true, oid: pgtype.BoolOID, want: true},
		{name: "integer", value: int32(42), oid: pgtype.Int4OID, want: int32(42)},
		{name: "date", value: timestamp, oid: pgtype.DateOID, want: "2025-01-02"},
		{name: "timestamp", value: timestamp, oid: pgtype.TimestampOID, want: "2025-01-02T03:04:05.123456789"},
		{name: "timestamptz", value: timestamp, oid: pgtype.TimestamptzOID, want: "2025-01-02T03:04:05.123456789+03:00"},
		{name: "uuid", value: uuid, oid: pgtype.UUIDOID, want: "00112233-4455-6677-8899-aabbccddeeff"},
		{name: "bytea", value: []byte{0, 1, 255}, oid: pgtype.ByteaOID, want: "AAH/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jsonValue(tt.value, tt.oid)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("value = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestJSONValueKeepsJSONNumberPrecision(t *testing.T) {
	value, err := jsonValue([]byte(`{"big":9223372036854775807,"nested":[true,null]}`), pgtype.JSONBOID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"big":9223372036854775807,"nested":[true,null]}` {
		t.Errorf("JSON = %s", encoded)
	}
}

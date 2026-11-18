package records

import (
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func scanRecord(rows pgx.Rows) (map[string]any, error) {
	fields := rows.FieldDescriptions()
	values := make([]any, len(fields))
	scanTargets := make([]any, len(fields))
	for index, field := range fields {
		if field.DataTypeOID == pgtype.JSONOID || field.DataTypeOID == pgtype.JSONBOID {
			scanTargets[index] = new([]byte)
		} else {
			scanTargets[index] = &values[index]
		}
	}
	if err := rows.Scan(scanTargets...); err != nil {
		return nil, err
	}

	record := make(map[string]any, len(values))
	for index, field := range fields {
		value := values[index]
		if rawJSON, ok := scanTargets[index].(*[]byte); ok {
			if *rawJSON == nil {
				value = nil
			} else {
				value = *rawJSON
			}
		}
		jsonValue, err := jsonValue(value, field.DataTypeOID)
		if err != nil {
			return nil, fmt.Errorf("convert column %q: %w", field.Name, err)
		}
		record[field.Name] = jsonValue
	}
	return record, nil
}

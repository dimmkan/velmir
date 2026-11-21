package records

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"velmir/backend/internal/introspection"
)

const (
	DefaultLimit = 50
	MaxLimit     = 100
	maxFilters   = 20
)

type ListOptions struct {
	Page          int64
	Limit         int
	Sort          string
	Direction     string
	Search        string
	SearchColumns []string
	Filters       []Filter
}

type Filter struct {
	Column   string
	Operator string
	Value    any
	NotNull  bool
}

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

type filterInput struct {
	Column   string          `json:"column"`
	Operator string          `json:"operator"`
	Value    json.RawMessage `json:"value"`
}

var (
	numericPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
	uuidPattern    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

func ParseListOptions(query url.Values, resource introspection.Resource) (ListOptions, error) {
	options := ListOptions{Page: 1, Limit: DefaultLimit, Direction: "asc"}

	page, present, err := oneValue(query, "page", "INVALID_PAGE")
	if err != nil {
		return ListOptions{}, err
	}
	if present {
		parsed, parseErr := strconv.ParseUint(page, 10, 64)
		if parseErr != nil || parsed == 0 || parsed > math.MaxInt64 {
			return ListOptions{}, validationError("INVALID_PAGE", "page must be a positive integer")
		}
		options.Page = int64(parsed)
	}

	limit, present, err := oneValue(query, "limit", "INVALID_LIMIT")
	if err != nil {
		return ListOptions{}, err
	}
	if present {
		parsed, parseErr := strconv.Atoi(limit)
		if parseErr != nil || parsed < 1 || parsed > MaxLimit {
			return ListOptions{}, validationError("INVALID_LIMIT", fmt.Sprintf("limit must be an integer from 1 to %d", MaxLimit))
		}
		options.Limit = parsed
	}
	if uint64(options.Page-1) > math.MaxInt64/uint64(options.Limit) {
		return ListOptions{}, validationError("INVALID_PAGE", "page is too large")
	}

	if options.Sort, _, err = oneValue(query, "sort", "INVALID_SORT"); err != nil {
		return ListOptions{}, err
	}
	if options.Sort != "" {
		column, ok := findColumn(resource, options.Sort)
		if !ok {
			return ListOptions{}, validationError("INVALID_SORT", fmt.Sprintf("unknown sort column %q", options.Sort))
		}
		if !isSortableType(column.DataType) {
			return ListOptions{}, validationError("INVALID_SORT", fmt.Sprintf("column %q does not support sorting", options.Sort))
		}
	}

	if direction, present, err := oneValue(query, "direction", "INVALID_DIRECTION"); err != nil {
		return ListOptions{}, err
	} else if present {
		if direction != "asc" && direction != "desc" {
			return ListOptions{}, validationError("INVALID_DIRECTION", "direction must be asc or desc")
		}
		options.Direction = direction
	}

	if options.Search, _, err = oneValue(query, "search", "INVALID_SEARCH"); err != nil {
		return ListOptions{}, err
	}
	if len(query["search_column"]) > len(resource.Columns) {
		return ListOptions{}, validationError("INVALID_SEARCH", "too many search columns")
	}
	seenSearchColumns := make(map[string]bool)
	for _, name := range query["search_column"] {
		column, ok := findColumn(resource, name)
		if !ok {
			return ListOptions{}, validationError("INVALID_SEARCH", fmt.Sprintf("unknown search column %q", name))
		}
		if !isTextType(column.DataType) {
			return ListOptions{}, validationError("INVALID_SEARCH", fmt.Sprintf("column %q is not searchable", name))
		}
		if !seenSearchColumns[name] {
			options.SearchColumns = append(options.SearchColumns, name)
			seenSearchColumns[name] = true
		}
	}
	if options.Search != "" && len(options.SearchColumns) == 0 {
		for _, column := range resource.Columns {
			if isTextType(column.DataType) {
				options.SearchColumns = append(options.SearchColumns, column.Name)
			}
		}
		if len(options.SearchColumns) == 0 {
			return ListOptions{}, validationError("INVALID_SEARCH", "resource has no searchable text columns")
		}
	}

	filterJSON, present, err := oneValue(query, "filter", "INVALID_FILTER")
	if err != nil {
		return ListOptions{}, err
	}
	if present && filterJSON != "" {
		options.Filters, err = parseFilters(filterJSON, resource)
		if err != nil {
			return ListOptions{}, err
		}
	}
	if options.Filters == nil {
		options.Filters = make([]Filter, 0)
	}
	if options.SearchColumns == nil {
		options.SearchColumns = make([]string, 0)
	}
	return options, nil
}

func oneValue(query url.Values, key, code string) (string, bool, error) {
	values, ok := query[key]
	if !ok {
		return "", false, nil
	}
	if len(values) != 1 {
		return "", false, validationError(code, key+" must be specified once")
	}
	return values[0], true, nil
}

func parseFilters(raw string, resource introspection.Resource) ([]Filter, error) {
	if len(raw) > 64*1024 {
		return nil, validationError("INVALID_FILTER", "filter parameter is too large")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var inputs []filterInput
	if err := decoder.Decode(&inputs); err != nil {
		return nil, validationError("INVALID_FILTER", "filter must be a JSON array of filter objects")
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return nil, validationError("INVALID_FILTER", "filter must contain one JSON value")
	}
	if len(inputs) > maxFilters {
		return nil, validationError("INVALID_FILTER", fmt.Sprintf("no more than %d filters are allowed", maxFilters))
	}

	filters := make([]Filter, 0, len(inputs))
	for _, input := range inputs {
		column, ok := findColumn(resource, input.Column)
		if !ok {
			return nil, validationError("INVALID_FILTER", fmt.Sprintf("unknown filter column %q", input.Column))
		}
		if input.Operator == "is_null" {
			var value bool
			if err := json.Unmarshal(input.Value, &value); err != nil {
				return nil, validationError("INVALID_FILTER", fmt.Sprintf("is_null filter for %q requires a boolean value", input.Column))
			}
			filters = append(filters, Filter{Column: input.Column, Operator: input.Operator, NotNull: !value})
			continue
		}
		if !supportsFilter(column.DataType, input.Operator) {
			return nil, validationError("INVALID_FILTER", fmt.Sprintf("operator %q is not supported for column %q", input.Operator, input.Column))
		}
		var rawValue string
		if err := json.Unmarshal(input.Value, &rawValue); err != nil {
			return nil, validationError("INVALID_FILTER", fmt.Sprintf("filter for %q requires a string value", input.Column))
		}
		value, err := parseFilterValue(column.DataType, rawValue)
		if err != nil {
			return nil, validationError("INVALID_FILTER", fmt.Sprintf("invalid value for column %q: %v", input.Column, err))
		}
		filters = append(filters, Filter{Column: input.Column, Operator: input.Operator, Value: value})
	}
	return filters, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("extra JSON value")
}

func parseFilterValue(dataType, value string) (any, error) {
	switch baseType(dataType) {
	case "text", "character varying", "character", "citext", "name":
		return value, nil
	case "boolean":
		if value != "true" && value != "false" {
			return nil, errors.New("expected true or false")
		}
		return value == "true", nil
	case "smallint":
		parsed, err := strconv.ParseInt(value, 10, 16)
		return int16(parsed), err
	case "integer":
		parsed, err := strconv.ParseInt(value, 10, 32)
		return int32(parsed), err
	case "bigint":
		parsed, err := strconv.ParseInt(value, 10, 64)
		return parsed, err
	case "real", "double precision":
		bits := 64
		if baseType(dataType) == "real" {
			bits = 32
		}
		parsed, err := strconv.ParseFloat(value, bits)
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return nil, errors.New("expected a finite number")
		}
		return parsed, nil
	case "numeric", "decimal":
		if !validNumeric(value) {
			return nil, errors.New("expected a decimal number in PostgreSQL numeric range")
		}
		return value, nil
	case "date":
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return nil, errors.New("expected a date in YYYY-MM-DD format")
		}
		return parsed, nil
	case "timestamp without time zone":
		parsed, err := time.Parse("2006-01-02T15:04:05.999999999", value)
		if err != nil {
			return nil, errors.New("expected a timestamp in YYYY-MM-DDTHH:MM:SS format")
		}
		return parsed, nil
	case "timestamp with time zone":
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, errors.New("expected an RFC 3339 timestamp")
		}
		return parsed, nil
	case "uuid":
		if !uuidPattern.MatchString(value) {
			return nil, errors.New("expected a UUID")
		}
		return value, nil
	default:
		return nil, errors.New("type does not support value filters")
	}
}

func validNumeric(value string) bool {
	if len(value) > 10000 || !numericPattern.MatchString(value) {
		return false
	}
	index := strings.IndexAny(value, "eE")
	if index < 0 {
		return true
	}
	exponent, err := strconv.ParseInt(value[index+1:], 10, 32)
	return err == nil && exponent >= -131072 && exponent <= 131072
}

func supportsFilter(dataType, operator string) bool {
	if operator == "is_null" {
		return true
	}
	if isTextType(dataType) {
		return operator == "eq" || operator == "neq" || operator == "contains"
	}
	switch baseType(dataType) {
	case "boolean":
		return operator == "eq" || operator == "neq"
	case "smallint", "integer", "bigint", "real", "double precision", "numeric", "decimal", "date", "timestamp without time zone", "timestamp with time zone", "uuid":
		return operator == "eq" || operator == "neq" || operator == "lt" || operator == "lte" || operator == "gt" || operator == "gte"
	default:
		return false
	}
}

func findColumn(resource introspection.Resource, name string) (introspection.Column, bool) {
	for _, column := range resource.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return introspection.Column{}, false
}

func isTextType(dataType string) bool {
	switch baseType(dataType) {
	case "text", "character varying", "character", "citext", "name":
		return true
	default:
		return false
	}
}

func isSortableType(dataType string) bool {
	switch baseType(dataType) {
	case "boolean", "smallint", "integer", "bigint", "real", "double precision", "numeric", "decimal", "date", "timestamp without time zone", "timestamp with time zone", "uuid", "bytea", "jsonb":
		return true
	default:
		return isTextType(dataType)
	}
}

func baseType(dataType string) string {
	normalized := strings.ToLower(strings.TrimSpace(dataType))
	if index := strings.IndexByte(normalized, '('); index >= 0 {
		normalized = normalized[:index] + normalized[strings.IndexByte(normalized[index:], ')')+index+1:]
	}
	return strings.TrimSpace(normalized)
}

func validationError(code, message string) error {
	return &ValidationError{Code: code, Message: message}
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func RejectUnreadableQueryFields(query url.Values, resource introspection.Resource, canRead func(string) bool) error {
	for _, key := range []string{"sort", "search_column"} {
		for _, name := range query[key] {
			if _, known := findColumn(resource, name); known && !canRead(name) {
				return validationError("FIELD_NOT_READABLE", "A query field is not readable")
			}
		}
	}
	var filters []struct {
		Column string `json:"column"`
	}
	if raw := query.Get("filter"); raw != "" && json.Unmarshal([]byte(raw), &filters) == nil {
		for _, filter := range filters {
			if _, known := findColumn(resource, filter.Column); known && !canRead(filter.Column) {
				return validationError("FIELD_NOT_READABLE", "A filter field is not readable")
			}
		}
	}
	return nil
}

func RestrictListOptions(options ListOptions, explicitSearchColumns bool, canRead func(string) bool) (ListOptions, error) {
	if options.Sort != "" && !canRead(options.Sort) {
		return ListOptions{}, validationError("FIELD_NOT_READABLE", "The sort field is not readable")
	}
	for _, filter := range options.Filters {
		if !canRead(filter.Column) {
			return ListOptions{}, validationError("FIELD_NOT_READABLE", "A filter field is not readable")
		}
	}
	if explicitSearchColumns {
		for _, column := range options.SearchColumns {
			if !canRead(column) {
				return ListOptions{}, validationError("FIELD_NOT_READABLE", "A search field is not readable")
			}
		}
	} else if options.Search != "" {
		readable := make([]string, 0, len(options.SearchColumns))
		for _, column := range options.SearchColumns {
			if canRead(column) {
				readable = append(readable, column)
			}
		}
		options.SearchColumns = readable
		if len(readable) == 0 {
			return ListOptions{}, validationError("INVALID_SEARCH", "resource has no readable searchable text columns")
		}
	}
	return options, nil
}

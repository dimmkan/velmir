# API

## GET /api/schema

Returns user schemas, tables and views from the configured target database. System schemas are excluded. The endpoint reads PostgreSQL metadata only.

Successful response (HTTP 200):

```json
{
  "write_enabled": true,
  "schemas": [
    {
      "name": "public",
      "resources": [
        {
          "name": "items",
          "kind": "table",
          "columns": [
            {
              "name": "id",
              "data_type": "bigint",
              "nullable": false,
              "default": "nextval('items_id_seq'::regclass)",
              "primary_key": true,
              "identity": "always",
              "generated": false,
              "editable": false,
              "enum_values": []
            }
          ],
          "primary_key": ["id"],
          "foreign_keys": [
            {
              "name": "items_owner_id_fkey",
              "columns": ["owner_id"],
              "referenced_schema": "public",
              "referenced_table": "owners",
              "referenced_columns": ["id"]
            }
          ]
        }
      ]
    }
  ]
}
```

`kind` is `table` or `view`. `default` is `null` when no default exists. `primary_key` marks a key column. `identity` is an empty string, `always` or `by_default`; `generated` marks generated columns. `editable` is true only for supported, non-generated table columns (an `always` identity is not editable). `enum_values` contains allowed labels for enum types and is empty otherwise. Composite primary and foreign keys preserve column order. Empty collections are returned as arrays.

`write_enabled` reports whether the optional target write pool is configured. It does not guarantee that every resource or column is writable.

If no target database is configured, the endpoint returns HTTP 503:

```json
{
  "error": {
    "code": "TARGET_NOT_CONFIGURED",
    "message": "Target database is not configured"
  }
}
```

Other introspection failures return HTTP 500 with code `SCHEMA_INTROSPECTION_FAILED`.

## GET /api/resources/{schema}/{resource}/records

Returns one page of records from a table or view. `schema` and `resource` must exactly match metadata returned by `GET /api/schema`; URL path encoding is supported. Every request uses fresh introspection metadata. Unknown schemas, resources and columns are rejected before SQL execution.

The endpoint is read-only. Dynamic identifiers are quoted after metadata validation, and all search and filter values are sent as PostgreSQL query parameters. SQL expressions in `sort`, `direction`, `search`, `search_column` and `filter` are not supported.

### Query parameters

- `page`: positive integer, default `1`.
- `limit`: integer from `1` to `100`, default `50`.
- `sort`: one allowed column name. No expression or comma-separated sorting is accepted.
- `direction`: `asc` or `desc`, default `asc`. Primary-key tie-breakers use the same direction.
- `search`: literal, case-insensitive substring searched with `ILIKE`. `%`, `_` and `\` are escaped and have no wildcard meaning.
- `search_column`: repeatable text-column name. By default, `search` covers every supported text column in the resource. Non-text and unknown columns are rejected.
- `filter`: URL-encoded JSON array with at most 20 objects. The encoded value must not exceed 64 KiB. Each object has `column`, `operator` and `value`:

```json
[
  {"column": "title", "operator": "contains", "value": "postgres"},
  {"column": "id", "operator": "gte", "value": "100"},
  {"column": "published_on", "operator": "is_null", "value": false}
]
```

Comparison values are JSON strings, even for booleans and numbers, so large integers and decimals do not pass through JavaScript numbers. `is_null` takes a JSON boolean: `true` means `IS NULL`, `false` means `IS NOT NULL`. Multiple filters are combined with `AND`.

Supported filters by PostgreSQL type:

- `text`, `character varying`, `character`, `citext`, `name`: `eq`, `neq`, `contains`, `is_null`.
- `boolean`: `eq`, `neq`, `is_null`; values are `true` or `false`.
- `smallint`, `integer`, `bigint`, `real`, `double precision`, `numeric`, `decimal`: `eq`, `neq`, `lt`, `lte`, `gt`, `gte`, `is_null`.
- `date`: the same comparison operators; values use `YYYY-MM-DD`.
- `timestamp without time zone`: the same comparison operators; values use `YYYY-MM-DDTHH:MM:SS` with optional fractional seconds.
- `timestamp with time zone`: the same comparison operators; values use RFC 3339.
- `uuid`: the same comparison operators; values use canonical UUID format.
- `json`, `jsonb`, `bytea` and other types: `is_null` only.

Sorting is available for text, boolean, integer, floating-point, numeric, date, timestamp, UUID, `bytea` and `jsonb` columns. Other types, including `json`, are not sortable through this endpoint.

### Successful response

HTTP 200:

```json
{
  "records": [
    {
      "id": "9223372036854775807",
      "price": "12345678901234567890.01",
      "active": true,
      "published_on": "2025-01-02",
      "created_at": "2025-01-02T03:04:05.123456789Z",
      "external_id": "00112233-4455-6677-8899-aabbccddeeff",
      "metadata": {"edition": 1, "big": 9223372036854775807},
      "payload": "AAH/",
      "description": null
    }
  ],
  "page": 1,
  "limit": 50,
  "has_next": false
}
```

Record keys are column names from `/api/schema`. JSON representations are:

- SQL `NULL` → JSON `null`.
- `boolean` → JSON boolean.
- `smallint`, `integer`, `real`, `double precision` → JSON number; non-finite floating-point values (`NaN`, infinities) → JSON string.
- `bigint`, `numeric`, `decimal` → JSON string, preserving integer and decimal precision.
- `date` → `YYYY-MM-DD` string.
- timestamps → ISO 8601/RFC 3339 strings with fractional seconds when present.
- `uuid` → canonical UUID string.
- `json`, `jsonb` → original JSON value; JSON numbers are not decoded through JavaScript `number`.
- `bytea` → standard Base64 string.
- Other PostgreSQL types → a safe string representation.

### Pagination

The query fetches `limit + 1` rows to calculate `has_next`; no `COUNT(*)` or total record count is returned. Offset is `(page - 1) * limit`. Large offsets can be expensive.

When a primary key exists, its columns are appended to the requested sort as unique tie-breakers. Without a requested sort, the primary key is the default ordering. Resources without a primary key have no guaranteed stable ordering between pages, especially when underlying data changes.

### Errors

Errors use the same `error.code` and `error.message` envelope as schema introspection:

- HTTP 400: `INVALID_PAGE`, `INVALID_LIMIT`, `INVALID_SORT`, `INVALID_DIRECTION`, `INVALID_SEARCH`, `INVALID_FILTER` or `INVALID_QUERY`.
- HTTP 404: `RESOURCE_NOT_FOUND` for a schema or resource absent from introspection metadata.
- HTTP 503: `TARGET_NOT_CONFIGURED`.
- HTTP 500: `SCHEMA_INTROSPECTION_FAILED` or `RECORDS_QUERY_FAILED`.

Records and introspection queries have a 10-second backend timeout and use the request context, so client cancellation propagates to PostgreSQL.

## POST /api/resources/{schema}/{resource}/records

Creates a record in a table. This endpoint is available only when `TARGET_WRITE_DATABASE_URL` is configured; introspection and identifier validation still use fresh metadata read through `TARGET_DATABASE_URL`.

Request body:

```json
{
  "values": {
    "title": "PostgreSQL Guide",
    "author_id": "1",
    "price": "12345678901234567890.01",
    "active": true,
    "published_on": "2025-01-02",
    "metadata": {"edition": 1}
  }
}
```

Only fields present in `values` are inserted. Omitted generated columns, identity columns and columns with defaults are left to PostgreSQL. A client can use `DEFAULT VALUES` behavior by sending an empty `values` object when all required fields have defaults or are nullable. JSON `null` means SQL `NULL` and is rejected for non-nullable columns. Text empty strings remain empty strings; they are not converted to `NULL`.

`bigint`, `numeric` and `decimal` values should be JSON strings to preserve precision. Integer values may be JSON numbers or strings. Dates, timestamps and UUIDs use the same formats as record filters. JSON and JSONB accept any valid JSON value. Enum values must match `enum_values`. Unsupported PostgreSQL types, generated columns and `always` identity columns cannot be supplied.

Successful response is HTTP 201. PostgreSQL `RETURNING` provides the complete saved record using the same safe JSON representation as the list endpoint:

```json
{
  "record": {
    "id": "9223372036854775807",
    "title": "PostgreSQL Guide"
  }
}
```

## PATCH /api/resources/{schema}/{resource}/records/{key}

Updates only the supplied editable, non-primary-key fields and returns the complete updated record with HTTP 200. Views and tables without a primary key are read-only for updates. Primary-key updates are not supported.

`{key}` is a **URL-encoded JSON object** containing exactly the resource's primary-key columns. Object order is not significant. Values use the request JSON representation, including strings for bigint and numeric keys. Examples before URL encoding:

```json
{"id":"42"}
```

```json
{"book_id":"10","category_id":"20"}
```

For example, `{"id":"42"}` is encoded as `%7B%22id%22%3A%2242%22%7D`. Missing, null, additional or unknown key fields return `INVALID_RECORD_KEY`.

The request and success bodies have the same shape as creation. An empty `values` object is rejected. Updates have **last write wins** semantics: there is no version column, optimistic lock or concurrent-change detection in this iteration.

### Mutation errors

The error envelope is shared with read endpoints. Validation and some constraint errors additionally include `field_errors`, keyed by column name:

```json
{
  "error": {
    "code": "INVALID_RECORD",
    "message": "Record validation failed",
    "field_errors": {"published_on": "Expected a date in YYYY-MM-DD format"}
  }
}
```

- HTTP 400: `INVALID_RECORD`, `INVALID_RECORD_KEY`, `NOT_NULL_VIOLATION`, `FOREIGN_KEY_VIOLATION` or `CHECK_VIOLATION`.
- HTTP 403: `TARGET_READ_ONLY` when `TARGET_WRITE_DATABASE_URL` is absent. The read pool remains read-only.
- HTTP 404: `RESOURCE_NOT_FOUND` or `RECORD_NOT_FOUND`.
- HTTP 409: `RESOURCE_READ_ONLY` or `UNIQUE_VIOLATION`.
- HTTP 503: `TARGET_NOT_CONFIGURED`.
- HTTP 500: `SCHEMA_INTROSPECTION_FAILED` or `RECORD_MUTATION_FAILED`.

Constraint responses use generic messages and do not expose SQL, stack traces or server-internal details. Mutation requests are limited to 1 MiB and have a 10-second backend timeout. Delete endpoints are not implemented.

## GET /readyz

Returns `admin_database` and `target_database` states. Each state is `ready`, `unavailable` or `not_configured`. A valid JSON body describes component states even when the HTTP status is 503.

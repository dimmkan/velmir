# API

## GET /api/schema

Returns user schemas, tables and views from the configured target database. System schemas are excluded. The endpoint reads PostgreSQL metadata only.

Successful response (HTTP 200):

```json
{
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
              "default": "nextval('items_id_seq'::regclass)"
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

`kind` is `table` or `view`. `default` is `null` when no default exists. Composite primary and foreign keys preserve column order. Empty collections are returned as arrays.

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

## GET /readyz

Returns `admin_database` and `target_database` states. Each state is `ready`, `unavailable` or `not_configured`. A valid JSON body describes component states even when the HTTP status is 503.

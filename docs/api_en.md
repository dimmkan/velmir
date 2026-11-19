# API

[Русская версия](api.md)

## Authentication and authorization

`POST /api/auth/login` is public. Every other `/api` endpoint requires a valid server-side session, including schema and record reads. `/healthz` and `/readyz` remain public for infrastructure checks. Unauthenticated requests receive HTTP 401 with a JSON error envelope:

```json
{
  "error": {
    "code": "UNAUTHENTICATED",
    "message": "Authentication required"
  }
}
```

Access is denied by default and enforced by the backend independently of the frontend. A user can have multiple roles, whose permissions are unioned. The `admin` system role grants `read`, `create`, `update` and `delete` on every resource; `viewer` grants only `read`. A resource-level grant consists of a schema, resource and operation; `*` in system roles matches every schema and resource. Unknown roles and missing roles grant no additional access. The `delete` grant is independent: `read`, `create`, `update` and `write_enabled` never imply it.

An authenticated user without the required permission receives HTTP 403 with code `FORBIDDEN`, distinct from HTTP 401 for a missing session and from `CSRF_TOKEN_INVALID`. `GET /api/auth/me` remains available and returns an empty permissions list to a user without a role. Public registration is unavailable. User and role administration is restricted to the `admin` system role.

### POST /api/auth/login

Request body:

```json
{
  "email": "admin@example.com",
  "password": "the-user-supplied-password"
}
```

A successful response is HTTP 200:

```json
{
  "user": {
    "id": 1,
    "email": "admin@example.com",
    "display_name": "Administrator"
  },
  "csrf_token": "a-random-token-returned-by-the-server",
  "can_admin": true,
  "permissions": [
    {
      "schema": "*",
      "resource": "*",
      "operations": ["read", "create", "update", "delete"]
    }
  ]
}
```

Each login creates a new session. Passwords are verified with Argon2id. Session tokens contain 32 cryptographically random bytes and only their SHA-256 hashes are stored in the admin database. JWTs are not used.

Unknown email addresses, incorrect passwords and inactive accounts return the same HTTP 401 response with code `INVALID_CREDENTIALS` and message `Invalid email or password`. Repeated failures are limited per source IP and per IP/email pair; HTTP 429 uses code `AUTH_RATE_LIMITED` without identifying an account.

### GET /api/auth/me

Returns HTTP 200 with the current user, CSRF token and effective permissions in the same shape as login. `can_admin` is true only for the assigned `admin` system role; wildcard permissions on a custom role do not enable administration. Each permissions entry contains `schema`, `resource` and an `operations` array; duplicate grants from multiple roles are already merged. A user without a role receives `"permissions": []` and `"can_admin": false`. The endpoint refreshes idle session expiration. A missing CSRF cookie is replaced. HTTP 401 means the session is missing, revoked, idle-expired or absolute-expired.

### POST /api/auth/logout

Requires the session cookie and a valid CSRF header. It revokes only the current session, clears both authentication cookies and returns HTTP 204 without a response body.

### Cookies and CSRF

- `velmir_session` is a session cookie with `Path=/`, `HttpOnly` and `SameSite=Lax`. It contains the random session token and is never exposed to JavaScript.
- `velmir_csrf` has `Path=/` and `SameSite=Lax`, but is not HttpOnly. Its value is also returned by login/me and kept only in frontend memory.
- Both cookies have `Secure` enabled when `SESSION_COOKIE_SECURE=true`; this must be enabled for HTTPS production deployments.
- The session cookie has no persistent browser expiry. Server-side idle expiration defaults to 30 minutes and absolute expiration to 24 hours. Configure them with `SESSION_IDLE_TIMEOUT` and `SESSION_ABSOLUTE_TIMEOUT` using Go duration syntax.

For every POST/PATCH/PUT/DELETE request and logout, send the login/me CSRF value in `X-CSRF-Token`. The server compares it with `velmir_csrf` using a constant-time comparison. A missing or mismatched value returns HTTP 403 with code `CSRF_TOKEN_INVALID`. Login is the unauthenticated entry point and does not require CSRF. The frontend uses same-origin requests; CORS wildcard access with credentials is not supported.

## User and role administration

Every endpoint below requires a session cookie and the `admin` system role. Every mutation also requires CSRF. Viewers, users without roles and custom roles with `*.*` receive 403 `FORBIDDEN`. Only the admin database is read or changed. Responses never expose password hashes, session tokens or token hashes.

| Method | Path | Purpose and body |
|---|---|---|
| GET | `/api/admin/users` | `{"users": [...]}`; each user has `id`, `email`, `display_name`, `is_active`, `roles` |
| POST | `/api/admin/users` | Create an active user without roles from `email`, `display_name`, `password`; HTTP 201 |
| PATCH | `/api/admin/users/{id}/display-name` | `{"display_name":"New name"}` |
| PATCH | `/api/admin/users/{id}/status` | `{"is_active":false}` or `true` |
| POST | `/api/admin/users/{id}/password-reset` | `{"password":"new-user-supplied-password"}` |
| PUT | `/api/admin/users/{id}/roles/{role_id}` | Assign a role; idempotent |
| DELETE | `/api/admin/users/{id}/roles/{role_id}` | Remove a role assignment; does not delete target records |
| GET | `/api/admin/roles` | `{"roles": [...]}`; each role has `id`, `name`, `description`, `is_system`, `permissions` |
| POST | `/api/admin/roles` | Create a custom role from `name`, `description`, `permissions`; HTTP 201 |
| PATCH | `/api/admin/roles/{id}` | Change a custom role's `name` and `description` |
| PUT | `/api/admin/roles/{id}/permissions` | Replace permissions with `{"permissions":[...]}` |

Successful mutations other than creation return HTTP 204. Disabling a user and resetting a password atomically revoke all of that user's sessions. Disabled users receive `INVALID_CREDENTIALS` at login. Self-disable and removing your own `admin` role are forbidden. The last active administrator cannot be disabled or lose admin rights, including by another administrator. System roles `admin` and `viewer` cannot be renamed or have permissions changed; role deletion is not exposed.

Permissions use `{"schema":"public","resource":"books","operations":["read","update"]}`. Only `read`, `create`, `update` and `delete` are supported; wildcard grants must set both schema and resource to `*`. An empty array removes all permissions from a custom role. Empty names, partial wildcards, duplicates and unknown operations are rejected. Custom role names start with a lowercase ASCII letter and contain at most 100 `a-z`, `0-9`, `_` or `-` characters; descriptions are limited to 500 characters. Email is limited to 254 characters, display name to 200 and password to 12–1024 characters. Every value is sent to PostgreSQL as a query parameter, never interpolated into SQL.

Role and assignment changes apply to subsequent requests without a backend restart, and multiple roles are unioned. Validation failures use HTTP 400 `INVALID_ADMIN_REQUEST` and optional `field_errors`. Conflicts include `ALREADY_EXISTS`, `LAST_ACTIVE_ADMIN`, `SELF_DISABLE_FORBIDDEN`, `SELF_ADMIN_REMOVAL_FORBIDDEN` and `SYSTEM_ROLE_IMMUTABLE`; missing entities return `USER_NOT_FOUND` or `ROLE_NOT_FOUND`.

## GET /api/schema

Returns only tables and views for which the current user has `read`. Inaccessible resources, schemas left empty by filtering and foreign keys referencing inaccessible resources are omitted. PostgreSQL system schemas are also excluded. Without any `read` grant, the endpoint returns HTTP 403 `FORBIDDEN` before introspection.

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

`write_enabled` is true only when the optional target write pool is configured and the current user has at least one `create`, `update` or `delete` grant. It does not guarantee that every resource or column is writable; frontend controls additionally use effective permissions.

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

The endpoint is read-only and requires `read` on the requested resource; otherwise it returns HTTP 403 `FORBIDDEN` before introspection. Dynamic identifiers are quoted after metadata validation, and all search and filter values are sent as PostgreSQL query parameters. SQL expressions in `sort`, `direction`, `search`, `search_column` and `filter` are not supported.

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

Creates a record in a table. It requires `create` on the resource and a valid CSRF token; without the permission it returns HTTP 403 `FORBIDDEN`. This endpoint is available only when `TARGET_WRITE_DATABASE_URL` is configured; introspection and identifier validation still use fresh metadata read through `TARGET_DATABASE_URL`.

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

Updates only the supplied editable, non-primary-key fields and returns the complete updated record with HTTP 200. It requires `update` and a valid CSRF token; without the permission it returns HTTP 403 `FORBIDDEN`. Views and tables without a primary key are read-only for updates. Primary-key updates are not supported.

`{key}` is a **URL-encoded JSON object** containing exactly the resource's primary-key columns. Object order is not significant. Values use the request JSON representation, including strings for bigint and numeric keys. Examples before URL encoding:

```json
{"id":"42"}
```

```json
{"book_id":"10","category_id":"20"}
```

For example, `{"id":"42"}` is encoded as `%7B%22id%22%3A%2242%22%7D`. Missing, null, additional or unknown key fields return `INVALID_RECORD_KEY`.

The request and success bodies have the same shape as creation. An empty `values` object is rejected. Updates have **last write wins** semantics: there is no version column, optimistic lock or concurrent-change detection in this iteration.

## DELETE /api/resources/{schema}/{resource}/records/{key}

Deletes one table record and returns HTTP 204 with no body. It requires a session, the independent `delete` permission on the resource and a valid CSRF token. The endpoint uses only `TARGET_WRITE_DATABASE_URL`; without a writer DSN it returns HTTP 403 `TARGET_READ_ONLY`. The read pool is never used for deletion.

`{key}` uses the same URL-encoded JSON format as PATCH and supports simple/composite PKs. Schema, resource and PK are checked against the fresh metadata allowlist. Identifiers are quoted and values are sent only as SQL parameters. Views and tables without a PK return `RESOURCE_READ_ONLY`. No request body is used. Bulk delete and deleting multiple records in one request are not supported.

The backend executes `DELETE … RETURNING` to obtain actual before values from the target database, not the client. A missing record returns 404 `RECORD_NOT_FOUND` without audit. Any failed DELETE, including an FK/constraint error, also creates no audit. Constraint responses contain no SQL, stack traces or server internals.

After the target commit, a `delete` event is written to the admin database with actual `before_data`, `after_data IS NULL`, the actor from the server session and the simple/composite record key. There is no cross-database atomicity. If DELETE committed but audit failed, the response is HTTP 500 `AUDIT_WRITE_FAILED` with `mutation_committed=true`; the record is already deleted. The backend does not retry DELETE or issue a compensating INSERT. The frontend shows a distinct localized warning and refreshes the list without repeating DELETE.

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
- HTTP 403: `FORBIDDEN` when the required permission is missing; `CSRF_TOKEN_INVALID` for a missing or invalid CSRF token; `TARGET_READ_ONLY` when `TARGET_WRITE_DATABASE_URL` is absent. The read pool remains read-only.
- HTTP 404: `RESOURCE_NOT_FOUND` or `RECORD_NOT_FOUND`.
- HTTP 409: `RESOURCE_READ_ONLY` or `UNIQUE_VIOLATION`.
- HTTP 503: `TARGET_NOT_CONFIGURED`.
- HTTP 500: `SCHEMA_INTROSPECTION_FAILED`, `RECORD_MUTATION_FAILED`, or `AUDIT_WRITE_FAILED` with `mutation_committed=true` when the target mutation committed but the audit write failed.

Constraint responses use generic messages and do not expose SQL, stack traces or server-internal details. POST/PATCH requests are limited to 1 MiB and have a 10-second backend timeout. DELETE accepts no body and deletes only one record.

After a successful POST, PATCH or DELETE, the backend writes a `create`, `update` or `delete` event to `audit_events` **in the admin database**. The actor comes from the server-side session, while the timestamp and UUID are generated by the server. Create stores the actual `after` returned by PostgreSQL `RETURNING`; update stores actual `before` and `after`; delete stores actual `before` from `DELETE … RETURNING` and `after = null`. Before UPDATE, the record is locked and read in the same target transaction, so `before` never comes from the client. Simple and composite primary keys are supported.

The target mutation and audit live in different databases and **are not one atomic transaction**. The target mutation commits first, then the audit event is created. A failed target mutation creates no event. If the target has committed but the audit write fails, the response is HTTP 500 with a distinct error; POST/PATCH responses include the saved record:

```json
{
  "error": {
    "code": "AUDIT_WRITE_FAILED",
    "message": "The target record was saved, but its audit event could not be recorded",
    "mutation_committed": true
  },
  "record": {"id": "42", "title": "Saved in target"}
}
```

`mutation_committed=true` means the target mutation has committed and **was not rolled back**. The backend performs no compensating DELETE/UPDATE/INSERT, two-phase commit or automatic POST/PATCH/DELETE retry. Clients must not automatically retry the mutation; refresh the list and inspect the state instead. For DELETE, the record is already gone, so the response has no `record`, and the frontend shows a distinct warning about deletion without guaranteed audit.

## Change history through Velmir

The `audit_events` table exists only in the admin database and contains:

- `id UUID PRIMARY KEY` — unique ID, default `gen_random_uuid()`;
- `occurred_at TIMESTAMPTZ` — server time, default `clock_timestamp()`;
- `actor_user_id BIGINT` — references `users(id)` for the actual authenticated actor;
- `action TEXT` — `create`, `update` or `delete`;
- `schema_name`, `resource_name` — technical resource identifiers;
- `record_key JSONB` — an object containing the simple or composite primary key;
- `before_data JSONB` — `NULL` for create and actual state for update/delete;
- `after_data JSONB` — actual state for create/update and `NULL` for delete.

A check constraint requires after for create, before/after for update and only before for delete. Indexes support queries scoped to one resource and key; there is no endpoint for unrestricted reads of the entire table. Goose migrations are transactional and use idempotent DDL. Velmir creates no audit tables, triggers or functions in the target database.

Fields whose names indicate passwords, session/CSRF tokens, password hashes, secrets or connection strings are omitted from audit, including nested JSON objects. Errors are logged by type only, without connection strings, record values or secrets. Bigint/numeric use the records API strings, bytea uses Base64, and JSON/JSONB is handled as `json.RawMessage`/`json.Number` rather than passing through JavaScript numbers.

History covers only successful changes made through Velmir after this feature was introduced. It **does not guarantee audit of external changes** made directly in the target database. Login/logout, reads and user administration are not audited. Create on a table without a primary key can store an event with an empty key, but record-specific history is unavailable.

## GET /api/resources/{schema}/{resource}/records/{key}/history

Returns one record's history in reverse chronological order (`occurred_at DESC`, then `id DESC`). It requires a session and `read` on the resource: no session returns 401 `UNAUTHENTICATED`; no read grant returns 403 `FORBIDDEN` before introspection or audit reads. Inaccessible resources and their history are not disclosed. GET does not require CSRF.

`{key}` uses the same URL-encoded JSON object as PATCH, including composite keys. Schema, resource and key columns are validated against fresh metadata allowlists. Query parameters:

- `page` — positive integer, default `1`; offset overflow is rejected;
- `limit` — from `1` to `100`, default `20`.

The response returns at most `limit` events and computes `has_next` by fetching `limit + 1`; it exposes neither a total count nor an unrestricted export. No history is HTTP 200 with an empty array:

```json
{
  "events": [
    {
      "id": "00112233-4455-6677-8899-aabbccddeeff",
      "actor": {"id": 1, "email": "admin@example.com", "display_name": "Administrator"},
      "timestamp": "2025-01-02T03:04:05.123456Z",
      "action": "update",
      "schema": "public",
      "resource": "items",
      "record_key": {"tenant_id": "7", "id": "42"},
      "before": {"title": "Before", "price": "1.00"},
      "after": {"title": "After", "price": "12345678901234567890.01"}
    }
  ],
  "page": 1,
  "limit": 20,
  "has_next": false
}
```

`before` is `null` for create, and `after` is `null` for delete. Deleted-record history remains available when its key is requested again; target-record existence is neither required nor checked. Record keys and before/after use the same safe JSON representation as the records API: exact bigint/numeric strings, JSON/JSONB without backend number conversion, Base64 bytea, and date/timestamp/UUID strings. Errors: 400 `INVALID_RECORD_KEY`, `INVALID_PAGE`, `INVALID_LIMIT`; 401/403 as above; 404 `RESOURCE_NOT_FOUND`; 500 `SCHEMA_INTROSPECTION_FAILED`, `HISTORY_QUERY_FAILED`; 503 `TARGET_NOT_CONFIGURED` or `AUDIT_UNAVAILABLE`.

## GET /readyz

Returns `admin_database` and `target_database` states. Each state is `ready`, `unavailable` or `not_configured`. A valid JSON body describes component states even when the HTTP status is 503.

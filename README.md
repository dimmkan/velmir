# Velmir

Velmir is a metadata-driven PostgreSQL admin panel. The current stage provides schema introspection, universal record lists with pagination, sorting, search and typed filters, plus automatic forms for creating and editing records. Deletion, authentication, authorization, relations UI and audit are not implemented.

## Requirements

- Go **1.26.0** (see `backend/go.mod`)
- Node.js and npm
- Docker with Docker Compose

## Local startup

From the repository root in PowerShell:

```powershell
Copy-Item .env.example .env
docker compose up -d admin-db target-db
```

Apply migrations to the admin database and start the backend:

```powershell
Set-Location backend
go run ./cmd/migrate
go run ./cmd/server
```

In a second terminal, start the frontend:

```powershell
Set-Location frontend
Copy-Item .env.example .env
npm ci
npm run dev
```

The backend listens on `http://127.0.0.1:8080` by default. It remains local-only unless `HTTP_ADDR` is explicitly changed. The frontend example environment points to that URL.

## Databases and write safety

- **Admin database** (`localhost:5432`, database `velmir_admin`) belongs to Velmir. Goose migrations run only here and currently provide the administrative migration baseline.
- **Demo target database** (`localhost:5433`, database `velmir_demo`) is a separate PostgreSQL instance. Docker initializes demonstration tables, records, a view, primary keys and foreign keys.
- `TARGET_DATABASE_URL` is the existing read-only target connection. Its pool always sets `default_transaction_read_only=on`; it is used for introspection and record reads and is never reused for mutations.
- `TARGET_WRITE_DATABASE_URL` is a separate **optional** connection used only for INSERT and UPDATE. The example points to the local demo writer role. Leave it empty or remove it to disable all mutation controls and make mutation endpoints return `TARGET_READ_ONLY`.
- Velmir never runs migrations, automatic GRANT statements or DDL against a target database. Do not point integration tests or either target URL at a real database for local demo setup.

The demo `velmir_writer` role has only:

- `CONNECT` on `velmir_demo` and `USAGE` on schema `public`;
- `SELECT`, `INSERT`, `UPDATE` on the existing demo tables;
- `USAGE`, `SELECT`, `UPDATE` on the existing demo sequences, required for identity/default sequence values.

It has no `CREATE`, `ALTER`, `DROP`, `TRUNCATE` or `DELETE` privilege. The `velmir_readonly` role retains read-only transactions and SELECT privileges.

To run without a target database, leave `TARGET_DATABASE_URL` empty. The backend still starts, readiness reports `not_configured`, and `/api/schema` returns `TARGET_NOT_CONFIGURED`.

## Creating and editing records

The frontend builds all forms from `/api/schema`; there are no table-specific forms. Supported inputs cover text, boolean, integer, bigint/numeric, date, timestamps, UUID, enums and JSON. Nullable fields have an explicit NULL control, and create forms can explicitly use a database default. Generated and `always` identity fields are not required or editable.

Updates require a primary key, including composite keys, and cannot change primary-key fields. Tables without a primary key allow creation but not updates. Views are read-only. Unsupported PostgreSQL types are displayed but not edited automatically. PostgreSQL not-null, unique, foreign-key and check violations are shown as API/form errors.

After saving, the current record list is refreshed. Forms warn about unsaved changes before closing or leaving the page. Concurrent updates currently have **last write wins** semantics; optimistic locking and conflict detection are not implemented. Deletion is intentionally unavailable.

See `docs/api.md` for mutation request bodies, record-key encoding and error codes.

## Demo target reset

Initialization scripts run only when Docker creates a new target data volume. This stage adds the demo writer role and a `book_categories.note` column, so an already initialized demo volume must be recreated to apply them.

The following commands destroy **only the local `velmir_demo` target container and its demo volume**. They do not remove the admin database volume. Do not use them for a real database:

```powershell
docker compose stop target-db
docker compose rm -f target-db
docker volume rm velmir_target-db-data
docker compose up -d target-db
```

If the Compose project name is not `velmir`, replace the volume prefix with the name shown by `docker volume ls`.

## Checks

```powershell
Set-Location backend
go test ./... -v -timeout 60s
go vet ./...
go build ./...

Set-Location ../frontend
npm test
npm run type-check
npm run build

Set-Location ..
docker compose config
```

Integration tests are excluded from unit tests by the `integration` build tag. Run them separately, only with the initialized local demo database:

```powershell
Set-Location backend
$env:TEST_TARGET_DATABASE_URL = "postgresql://velmir_readonly:velmir_readonly_local_only@localhost:5433/velmir_demo?sslmode=disable"
go test -tags=integration ./internal/introspection -v -timeout 30s
go test -tags=integration ./internal/records -v -timeout 30s

$env:TEST_TARGET_WRITE_DATABASE_URL = "postgresql://velmir_writer:velmir_writer_local_only@localhost:5433/velmir_demo?sslmode=disable"
go test -tags=integration ./internal/records -run TestWriterDemoDatabase -v -timeout 30s
```

The writer integration test creates and updates demo records and intentionally leaves them in the demo database; it does not delete records. It refuses to run against a non-local host or a database other than `velmir_demo`.

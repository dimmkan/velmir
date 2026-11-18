# Velmir

Velmir is a metadata-driven PostgreSQL admin panel. The current stage provides read-only schema introspection and universal record lists with pagination, sorting, search and typed filters. Editing, authentication and audit are not implemented.

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

The backend listens on `http://127.0.0.1:8080`. The frontend example environment points to that URL.

## Databases

- **Admin database** (`localhost:5432`, database `velmir_admin`) belongs to Velmir. Goose migrations run only here and currently provide the administrative migration baseline.
- **Demo target database** (`localhost:5433`, database `velmir_demo`) is a separate PostgreSQL instance. Docker initializes demonstration tables, records, a view, primary keys and foreign keys. Velmir connects as the local-only `velmir_readonly` role, which has read-only transactions and SELECT privileges.
- Velmir never applies migrations or writes to the target database. The target pool also sets `default_transaction_read_only=on`.

The demo initialization scripts run only when Docker creates a new target data volume.

To run without a target database, leave `TARGET_DATABASE_URL` empty. The backend still starts, readiness reports `not_configured`, and `/api/schema` returns `TARGET_NOT_CONFIGURED`.

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

The introspection and records integration tests are excluded from unit tests by the `integration` build tag. With the initialized demo database running:

```powershell
Set-Location backend
$env:TEST_TARGET_DATABASE_URL = "postgresql://velmir_readonly:velmir_readonly_local_only@localhost:5433/velmir_demo?sslmode=disable"
go test -tags=integration ./internal/introspection -v -timeout 30s
go test -tags=integration ./internal/records -v -timeout 30s
```

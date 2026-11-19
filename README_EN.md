# Velmir

[Русская версия](README.md)

Velmir is a metadata-driven PostgreSQL admin panel. The current stage provides schema introspection, universal record lists with pagination, sorting, search and typed filters, automatic forms for creating and editing records, create/update history for changes made through Velmir, server-side session authentication, RBAC and user/role management UI. Record deletion, relations UI, files and automation are not implemented.

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

Before the first backend start, set `VELMIR_BOOTSTRAP_ADMIN_EMAIL` and `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` in `.env` to a unique administrator email and a long random password of at least 12 characters. Do not use a default password. Apply migrations to the admin database and start the backend:

```powershell
Set-Location backend
go run ./cmd/migrate
go run ./cmd/server
```

When the admin database contains no users, server startup creates the first administrator once and logs a reminder without logging the password. **After the administrator is created, remove both bootstrap variables from `.env` and the process environment, then restart the backend.** Subsequent startups do not create another user while any user exists. Startup fails with an actionable error if the database is empty and bootstrap values are missing. Public registration is unavailable; administrators create additional accounts through the UI or CLI.

In a second terminal, start the frontend:

```powershell
Set-Location frontend
Copy-Item .env.example .env
npm ci
npm run dev
```

The backend listens on `http://127.0.0.1:8080` by default and remains local-only unless `HTTP_ADDR` is explicitly changed. Open the Vite URL (normally `http://localhost:5173`), not the backend URL. Vite proxies `/api`, `/healthz` and `/readyz` to `VITE_BACKEND_PROXY_TARGET`, so cookies and API requests use the browser's same origin. `VITE_BACKEND_BASE_URL` stays empty.

For production, serve the built frontend and backend API through the same HTTPS origin, with SPA fallback for `/login` and `/administration`; keep `VITE_BACKEND_BASE_URL` empty and set `SESSION_COOKIE_SECURE=true`. No CORS wildcard with credentials is used or required.

## Interface design and language

- The corporate palette consists of Velmir Deep `#0F6B5F`, Midnight Bark `#101B20`, Source Mist `#F4F7F5` and Mimir Gold `#C18A3D`. Semantic color tokens, including contrast variants for dark surfaces and error/success/disabled states, are centralized in `frontend/src/style.css`.
- Source SVG and PNG assets are stored in `frontend/public/brand/`. The full logo appears on the login page, the compact logo in the sidebar, and the mark on narrow screens; light/dark variants match the surface. CSS does not alter the brand mark's shape.
- SVG/PNG favicons, the Apple touch icon and `site.webmanifest` are linked in `frontend/index.html`. No service worker is used.
- The interface is available in Russian and English. Russian is the default; the shared footer switch changes the language without a reload and updates `html lang`. The explicit choice is stored only in `localStorage` under `velmir_locale` (`ru` or `en`) and requires no backend API. Namespaced translations live in `frontend/src/locales/`, with English as the fallback for missing strings.

## Authentication and sessions

The application calls `GET /api/auth/me` at startup, redirects anonymous users to `/login`, and restores the original route after login. Logout revokes only the current session. Record mutations and logout require the server-verified `X-CSRF-Token` header. Passwords, session tokens and CSRF tokens are not stored in `localStorage`; the session cookie is HttpOnly and the CSRF token remains in memory.

Session cookies use `SameSite=Lax`; idle expiration defaults to `30m` and absolute expiration to `24h`. See `docs/api_en.md` for cookie attributes, configuration and endpoint responses.

## Roles, permissions and user management

Access is denied by default. Roles can be combined: a user's effective permissions are the union of every assigned role. The built-in `admin` role allows reading, creating and updating every resource; `viewer` allows reading only. `read`, `create` and `update` grants can target a specific schema and resource. DELETE is unavailable to every role, including `admin`.

The bootstrap administrator receives `admin` automatically. A new user receives no role by default and can access only `/api/auth/me` with an empty permissions list; protected resources return 403. The backend filters `/api/schema` by `read`, and the frontend hides unavailable resources and create/update controls. Viewers see an explicit read-only state.

Administrators have a separate **Administration** section for creating users, changing display names, enabling/disabling accounts, resetting passwords and assigning roles, plus creating and editing custom roles and permissions. The section is hidden from viewers and users without roles, while the backend independently checks the `admin` system role, session and CSRF. Disabling an account and resetting a password revoke all user sessions. Self-disable, removing your own `admin` role and removing admin rights from the last active administrator are forbidden. System roles are immutable; wildcard `*.*` and concrete schema/resource grants support only `read`, `create` and `update`. Changes apply to subsequent requests without a restart. See `docs/api_en.md` for complete API contracts.

The `cmd/users` CLI remains available for local administration and access recovery. After applying migrations, run these commands from the `backend` directory:

```powershell
# Create a user; never pass the password as a command-line argument.
$env:VELMIR_USER_PASSWORD = "<long-random-password>"
go run ./cmd/users create -email "viewer@example.test" -display-name "Viewer"
Remove-Item Env:VELMIR_USER_PASSWORD

# Assign a role and list users.
go run ./cmd/users assign-role -email "viewer@example.test" -role viewer
go run ./cmd/users list
```

For a local viewer check, these two commands create the account and assign `viewer`. Sign in using a separate browser profile: schema and records GET are available, while create and edit controls are hidden. Repeating `assign-role` is idempotent; run it again with another role name to assign an additional role. Passwords must contain at least 12 characters, are accepted only through `VELMIR_USER_PASSWORD`, and are stored only as Argon2id hashes. Never put real passwords in documentation or command history.

## Databases and write safety

- **Admin database** (`localhost:5432`, database `velmir_admin`) belongs to Velmir. Goose migrations run only here and create the migration baseline plus `users`, `sessions`, `roles`, `user_roles`, `role_permissions` and `audit_events`.
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

See `docs/api_en.md` for mutation request bodies, record-key encoding and error codes.

## Create and update history

Every successful POST/PATCH through Velmir creates an `audit_events` row in the admin database with a UUID, server timestamp, actual actor, `create`/`update` action, schema/resource, simple or composite record key and safe before/after JSONB. Create stores after; update stores before and after from the actual target state. Passwords, tokens, secrets and connection strings are omitted. Bigint/numeric remain strings, bytea uses Base64, and JSON/JSONB preserves precision on the backend.

For a row with a primary key, **History** opens a generic panel with the actor, `Intl`-formatted time, action and changed fields. Russian and English switch without reload; pagination and mobile layouts are supported. `GET /api/resources/{schema}/{resource}/records/{key}/history` requires a session and `read`: no session returns 401, while no read grant returns 403 without a login redirect. Changes made outside Velmir and DELETE are not audited at this stage.

The target mutation commits in the target database before audit is written to the admin database; there is no cross-database atomicity. If audit fails after commit, the API returns `AUDIT_WRITE_FAILED` with `mutation_committed=true`, and the frontend explains that the change is already saved and offers a data refresh. There is no automatic POST/PATCH retry or compensating DELETE/UPDATE. See `docs/api_en.md` for the full schema, history contract and limitations.

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

$env:TEST_ADMIN_DATABASE_URL = "postgresql://velmir:velmir_local_only@localhost:5432/velmir_admin?sslmode=disable"
go test -tags=integration ./internal/auth -v -timeout 30s
go test -tags=integration ./internal/httpapi -v -timeout 60s

# Full run with all three local test URLs already set.
go test -tags=integration ./... -v -timeout 60s
Remove-Item Env:TEST_ADMIN_DATABASE_URL
Remove-Item Env:TEST_TARGET_DATABASE_URL
Remove-Item Env:TEST_TARGET_WRITE_DATABASE_URL
```

The writer and history integration tests create and update demo records and intentionally leave them in the demo database; records are not deleted. The tests refuse non-local hosts and databases other than `velmir_demo`.

The auth, audit storage and admin/history API integration tests require the migrated local `velmir_admin` database, reject non-local hosts and other database names, and roll back their admin transactions. Do not run them against a real admin database. Tests never alter the target schema.

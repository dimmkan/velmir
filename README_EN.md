# Velmir

[Русская версия](README.md)

Velmir is a metadata-driven PostgreSQL admin panel. The current stage provides schema introspection, universal record lists with pagination, sorting, search and typed filters, related labels, automatic forms with many-to-one relation selectors and read-only one-to-many panels, confirmed record deletion, change history for changes made through Velmir, server-side session authentication, RBAC and user/role management UI. A many-to-many editor, nested mutations, files and automation are not implemented.

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

Access is denied by default. Roles can be combined: a user's effective permissions are the union of every assigned role. The built-in `admin` role allows reading, creating, updating and deleting every resource; `viewer` allows reading only. `read`, `create`, `update` and `delete` grants can target a specific schema and resource. The `delete` grant is independent of other grants and writer availability.

The bootstrap administrator receives `admin` automatically. A new user receives no role by default and can access only `/api/auth/me` with an empty permissions list; protected resources return 403. The backend filters `/api/schema` by `read`, and the frontend hides unavailable resources and create/update/delete controls. Viewers see an explicit read-only state.

Administrators have a separate **Administration** section for creating users, changing display names, enabling/disabling accounts, resetting passwords and assigning roles, plus creating and editing custom roles and permissions. The section is hidden from viewers and users without roles, while the backend independently checks the `admin` system role, session and CSRF. Disabling an account and resetting a password revoke all user sessions. Self-disable, removing your own `admin` role and removing admin rights from the last active administrator are forbidden. System roles are immutable; wildcard `*.*` and concrete schema/resource grants support `read`, `create`, `update` and `delete`. A custom role receives `delete` only after an explicit assignment in PermissionEditor; the migration does not grant it to existing custom roles. Changes apply to subsequent requests without a restart. See `docs/api_en.md` for complete API contracts.

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
- `TARGET_WRITE_DATABASE_URL` is a separate **optional** connection used only for INSERT, UPDATE and DELETE. The example points to the local demo writer role. Leave it empty or remove it to disable all mutation controls and make mutation endpoints return `TARGET_READ_ONLY`.
- Velmir never runs migrations, automatic GRANT statements or DDL against a target database. Do not point integration tests or either target URL at a real database for local demo setup.

The demo `velmir_writer` role has only:

- `CONNECT` on `velmir_demo` and `USAGE` on schema `public`;
- `SELECT`, `INSERT`, `UPDATE`, `DELETE` on the existing demo tables;
- `USAGE`, `SELECT`, `UPDATE` on the existing demo sequences, required for identity/default sequence values.

It has no `CREATE`, `ALTER`, `DROP` or `TRUNCATE` privilege. The `velmir_readonly` role retains read-only transactions and SELECT privileges. For an already initialized **local-only** `velmir_demo`, the owner can grant DELETE once without recreating the volume:

```powershell
docker compose exec target-db psql -U velmir_target_admin -d velmir_demo -v ON_ERROR_STOP=1 -c "GRANT DELETE ON ALL TABLES IN SCHEMA public TO velmir_writer;"
```

Do not run this command against a real database. In production, the database owner configures the required privileges manually; the application never grants them automatically.

To run without a target database, leave `TARGET_DATABASE_URL` empty. The backend still starts, readiness reports `not_configured`, and `/api/schema` returns `TARGET_NOT_CONFIGURED`.

## Creating, editing and deleting records

The frontend builds all forms from `/api/schema`; there are no table-specific forms. Supported inputs cover text, boolean, integer, bigint/numeric, date, timestamps, UUID, enums and JSON. Nullable fields have an explicit NULL control, and create forms can explicitly use a database default. Generated and `always` identity fields are not required or editable.

Updates and deletes require a primary key, including composite keys, and updates cannot change primary-key fields. Tables without a primary key allow creation but not updates or deletes. Views are read-only. Unsupported PostgreSQL types are displayed but not edited automatically. PostgreSQL not-null, unique, foreign-key and check violations are shown as API/form errors.

### Many-to-one relation selectors

When the user has `read` on the referenced resource, forms generically replace raw FK inputs with selectors driven by `/api/schema` metadata. A simple FK uses one selector, while a composite FK uses one logical selector for every local/referenced column. Only the first 20-record page is loaded; search runs on the server with a 300 ms debounce, and **Load more** appends later pages. During editing, the current value is fetched separately through exact typed filters on every referenced column, so it is preserved even when absent from the first page.

Display labels use `display_name`, `name`, `title`, `label`, another non-empty text field, then the technical identifier, with every referenced key part shown alongside. The selector only fills existing FK columns in the unchanged POST/PATCH payload. A nullable relation can be cleared as a whole, a required one cannot, and a partially populated composite FK is never submitted. Bigint/numeric and UUID values retain their string representation, and NULL remains JSON `null`.

Without `read` on the referenced resource, related records and labels are not requested: the safe raw FK input remains, and available FK metadata does not expose the hidden resource. Read-only controls cannot change the relation. There are no nested mutations or automatic related-record creation/editing.

### Related labels and one-to-many panels

Many-to-one FKs in record lists use the shared label helper: `display_name`, `name`, `title`, `label`, another text field, then the technical identifier. The referenced key and every composite key part remain visible; NULL has a localized state, and failures retain the raw FK. Read-only `POST /api/resources/{schema}/{resource}/relations/resolve` batches deduplicated keys by relation and requires neither CSRF, a writer nor audit. Limits are 256 KiB, 20 relations, 200 keys per relation and 1000 in total. No per-row requests are made.

`/api/schema` also returns RBAC-filtered `reverse_relations` with the constraint name, child schema/resource and ordered local/referenced columns. Parent forms/details use them for generic read-only child panels with typed filters for every simple/composite key part, independent pagination and isolated loading/empty/401/403/error states. Details require only `read` and have no save controls; reverse relations whose types lack an existing `eq` records filter are currently hidden without a query. An unsaved parent shows **Save the record first** and runs no child query. Panels have no create/edit/delete controls and preserve parent values and pagination when the language changes.

Backend and frontend independently enforce permissions: read only on the parent hides labels/panels; read on parent and child enables them; read only on the referenced resource does not enable resolve for an inaccessible current resource. No session returns 401, while an authenticated user without permission receives 403 without a login redirect. See `docs/api_en.md` for the full batch contract, reverse metadata and permission matrix. A many-to-many editor, creating child records from a parent form and nested mutations are not implemented.

After saving, the current record list is refreshed. Forms warn about unsaved changes before closing or leaving the page. Concurrent updates currently have **last write wins** semantics; optimistic locking and conflict detection are not implemented.

**Delete** is available only with `delete` permission, a configured writer and a table with a PK. The accessible dialog displays the resource and exact simple/composite record identifier, warns about irreversibility and blocks repeat submission. After 204, the list refreshes; deleting the last record on a page navigates to the previous page. Bulk delete is not available: one request deletes exactly one record.

See `docs/api_en.md` for mutation request bodies, record-key encoding and error codes.

## Create, update and delete history

Every successful POST/PATCH/DELETE through Velmir creates an `audit_events` row in the admin database with a UUID, server timestamp, actual actor, `create`/`update`/`delete` action, schema/resource, simple or composite record key and safe before/after JSONB. Create stores after; update stores before and after; delete stores actual before values from `DELETE … RETURNING` and `after_data IS NULL`. Passwords, tokens, secrets and connection strings are omitted. Bigint/numeric remain strings, bytea uses Base64, and JSON/JSONB preserves precision on the backend.

For a row with a primary key, **History** opens a generic panel with the actor, `Intl`-formatted time, action and values. Russian and English switch without reload; pagination and mobile layouts are supported. `GET /api/resources/{schema}/{resource}/records/{key}/history` requires a session and `read`: no session returns 401, while no read grant returns 403 without a login redirect. Deleted-record history remains available by key without checking that the target record still exists. Changes made outside Velmir are not audited.

The target mutation commits in the target database before audit is written to the admin database; there is no cross-database atomicity. If audit fails after commit, the API returns `AUDIT_WRITE_FAILED` with `mutation_committed=true`, and the frontend explains that the change was applied and refreshes data. DELETE has a distinct warning: the record is already deleted, but its history event may not have been recorded. There is no automatic POST/PATCH/DELETE retry or compensating DELETE/UPDATE/INSERT. See `docs/api_en.md` for the full schema, history contract and limitations.

## Demo target reset

Initialization scripts run only when Docker creates a new target data volume. For DELETE on an already initialized local volume, use the one-time GRANT from “Databases and write safety”; recreating the volume is not required.

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

The writer and history integration tests create, update and delete demo records, including simple/composite PKs, FK errors and audit failure after commit. Some audit-failure fixtures intentionally remain in the demo database. The tests refuse non-local hosts and databases other than `velmir_demo`.

The auth, audit storage and admin/history API integration tests require the migrated local `velmir_admin` database, reject non-local hosts and other database names, and roll back their admin transactions. Do not run them against a real admin database. Tests never alter the target schema.

# Velmir

[Русская версия](README.md)

Velmir is a metadata-driven PostgreSQL admin panel. The current stage provides schema introspection, universal record lists with pagination, sorting, search and typed filters, related labels, automatic forms with many-to-one relation selectors, read-only one-to-many panels and a basic many-to-many editor through junction tables, confirmed record deletion, change history for changes made through Velmir, server-side session authentication, RBAC and user/role management UI. Nested mutations, files and automation are not implemented.

## Requirements

- Go **1.26.0** (see `backend/go.mod`)
- Node.js and npm
- Docker with Docker Compose

## Docker Compose

A single image has three stages: Node.js runs `npm ci`, type-check and the Vue build; Go builds the server, migrate, bootstrap and users CLIs; the minimal Alpine runtime contains only a non-root process, CA certificates, admin migrations and the compiled frontend. Frontend sources, `.env`, and Node/Go toolchains are not copied into runtime.

```text
admin-db ──healthy──> migrate ──completed──> bootstrap ──completed──> app
                                                                    ├─ /api/*
                                                                    └─ Vue + SPA fallback
The demo override also creates an isolated target-db; app uses one TARGET_DATABASE_URL.
```

### One-command demo

No `.env` setup is required for the demo:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml up --build -d
```

Open `http://127.0.0.1:8080`. With an empty admin database, the **localhost-only demo** creates login `admin` with password `admin`; a warning is logged without repeating the password. Do not expose this mode to a network or reuse demo credentials in production. The demo is enabled only by the explicit second `-f`; target is published at `127.0.0.1:5433` for local integration tests. Admin DB is not published.

### Production

1. Copy `.env.docker.example` to the root `.env`, replace placeholders, set a long random `POSTGRES_PASSWORD`, a matching `ADMIN_DATABASE_URL`, and secure bootstrap credentials. URL-encode passwords inside DSNs.
2. Optionally set `TARGET_DATABASE_URL`. Without it, auth/admin UI still works, but target resources are not configured.
3. Start the stack:

```bash
docker compose up --build -d
```

HTTP is published only as `127.0.0.1:${VELMIR_HTTP_PORT:-8080}`. Production expects an external HTTPS terminator on the same origin; `APP_ENV=production` requires `SESSION_COOKIE_SECURE=true` and the built frontend, and rejects insecure configuration. Direct HTTP is unsuitable for production login with Secure cookies. No credential-bearing wildcard CORS is used.

After the first successful start, remove `VELMIR_BOOTSTRAP_ADMIN_LOGIN`, `VELMIR_BOOTSTRAP_ADMIN_PASSWORD`, and legacy `VELMIR_BOOTSTRAP_ADMIN_EMAIL` from `.env` and the environment, then run `docker compose up -d`. While any user exists, bootstrap creates nothing, changes no password, and does not require these variables.

| Situation | Bootstrap result |
|---|---|
| Production, no users, secure login/password set | One admin with the `admin` role |
| Production, no users, missing/weak credentials or `admin/admin` | Failure; app does not start or become ready |
| Demo, no users, empty variables | `admin/admin`, only with `development + VELMIR_DEMO_MODE=true` |
| Users already exist | Nothing is created or reset; reruns are idempotent |

### Compose operations

```bash
docker compose ps
docker compose logs -f app
docker compose logs bootstrap
docker compose stop                 # keep containers and named volumes
docker compose down                 # remove containers/network, keep named volumes
docker compose up -d                # restart without rebuilding
docker compose up --build -d        # update the image from current sources
```

`admin-db-data` and demo `target-db-data` survive `stop`, `down`, restarts, and repeated migrate/bootstrap. Backup/restore is planned for H2.

### Complete environment table

The root `.env` is used for Docker Compose interpolation and by local Go CLIs through `godotenv`. `frontend/.env` is only for Vite development. Below, **dev** means local backend/Vite, **prod** means the main Compose file, and **demo** means the demo override. “Secret” means the value must not be committed or logged.

| Variable | Purpose | Required | Default | Allowed values | Mode | Secret | Safe example |
|---|---|---|---|---|---|---|---|
| `POSTGRES_DB` | Admin DB name | prod: yes; demo: fixed | none | PostgreSQL DB name | prod/demo | no | `velmir_admin` |
| `POSTGRES_USER` | Admin DB role | prod: yes; demo: fixed | none | PostgreSQL role name | prod/demo | no | `velmir` |
| `POSTGRES_PASSWORD` | Admin DB password | prod: yes; demo: local-only | none | Non-empty random password | prod/demo | **yes** | `<long-random-password>` |
| `ADMIN_DATABASE_URL` | Admin DSN for every Go process | prod/dev: yes; demo: fixed | none | PostgreSQL DSN | all | **yes** | `postgresql://velmir:<password>@admin-db:5432/velmir_admin?sslmode=disable` |
| `TARGET_DATABASE_URL` | Single DSN for read/mutation target pools | no | empty | PostgreSQL DSN | all | **yes** | `postgresql://app:<password>@target-db:5432/app?sslmode=require` |
| `VELMIR_DEPRECATED_TARGET_WRITE_DATABASE_URL` | Compose-only legacy value forwarding for migration error; not a pool DSN | no; do not set manually | empty | Any non-empty value fails | prod/demo compatibility | **yes** | empty |
| `APP_ENV` | Security mode | no | backend: `development`; Compose: `production` | `development`, `production` | all | no | `production` |
| `SESSION_COOKIE_SECURE` | Secure cookie flag | prod: `true` | backend: `false`; Compose: `true` | `true`, `false` | all | no | `true` |
| `VELMIR_BOOTSTRAP_ADMIN_LOGIN` | First admin login | Only for empty prod DB | none; demo: `admin` | Up to 254 characters, no whitespace | all | no | `admin` |
| `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` | First admin password | Only for empty prod DB | none; demo: `admin` | Prod: 12–1024, non-trivial | all | **yes** | `<long-random-password>` |
| `VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME` | First admin display name | no | `Administrator` | Up to 200 characters | all | no | `Administrator` |
| `VELMIR_BOOTSTRAP_ADMIN_EMAIL` | Legacy login fallback | no | empty | Same as login; conflicts are rejected | all | no | `admin@example.test` |
| `VELMIR_DEMO_MODE` | Allow demo-only bootstrap | Demo override only | `false` | `true` only with development | demo | no | `true` |
| `VELMIR_HTTP_PORT` | Host HTTP port | no | `8080` | 1–65535 | prod/demo | no | `8080` |
| `VELMIR_TARGET_PORT` | Host demo target port | no | `5433` | 1–65535 | demo | no | `5433` |
| `VELMIR_IMAGE` | Application image name | no | `velmir:local` | Docker image reference | prod/demo | no | `velmir:local` |
| `HTTP_ADDR` | Backend listen address | no | `127.0.0.1:8080`; Compose: `0.0.0.0:8080` | `host:port` | dev; fixed by Compose | no | `127.0.0.1:8080` |
| `FRONTEND_DIST_PATH` | Built Vue directory | prod: yes | dev: empty; image: `/app/frontend/dist` | Existing directory with `index.html` | all | no | `/app/frontend/dist` |
| `LOG_LEVEL` | Structured log level | no | `info` | `debug`, `info`, `warn`, `error` | all | no | `info` |
| `SESSION_IDLE_TIMEOUT` | Idle session lifetime | no | `30m` | Positive Go duration | all | no | `30m` |
| `SESSION_ABSOLUTE_TIMEOUT` | Absolute session lifetime | no | `24h` | Go duration, at least idle | all | no | `24h` |
| `DATABASE_CONNECT_TIMEOUT` | New DB connection timeout | no | `5s` | Positive Go duration | all | no | `5s` |
| `ADMIN_DATABASE_MAX_CONNS` | Max admin pool connections | no | `10` | Positive integer | all | no | `10` |
| `TARGET_DATABASE_MAX_CONNS` | Max connections per target pool | no | `10` | Positive integer | all | no | `10` |
| `DATABASE_MAX_CONN_LIFETIME` | Max connection lifetime | no | `30m` | Positive Go duration | all | no | `30m` |
| `DATABASE_MAX_CONN_IDLE_TIME` | Max idle connection time | no | `5m` | Positive Go duration | all | no | `5m` |
| `HTTP_READ_HEADER_TIMEOUT` | Read headers timeout | no | `5s` | Positive Go duration | all | no | `5s` |
| `HTTP_READ_TIMEOUT` | Read request timeout | no | `15s` | Positive Go duration | all | no | `15s` |
| `HTTP_WRITE_TIMEOUT` | Write response timeout | no | `30s` | Positive Go duration | all | no | `30s` |
| `HTTP_IDLE_TIMEOUT` | Keep-alive idle timeout | no | `60s` | Positive Go duration | all | no | `60s` |
| `HTTP_SHUTDOWN_TIMEOUT` | Graceful SIGTERM shutdown | no | `10s` | Positive Go duration | all | no | `10s` |
| `HTTP_MAX_JSON_BODY_SIZE` | Global JSON body limit | no | `1048576` | Positive number of bytes | all | no | `1048576` |

For `cmd/users`, the password is accepted only through the optional secret `VELMIR_USER_PASSWORD` (12–1024 characters), never a CLI argument. Frontend development additionally uses non-secret `VITE_BACKEND_BASE_URL=` (same-origin) and `VITE_BACKEND_PROXY_TARGET=http://127.0.0.1:8080` (Vite proxy only).

The production image builds the frontend with an empty same-origin API URL, so it does not need `VITE_BACKEND_BASE_URL`. Compose DSNs use the `admin-db` and `target-db` DNS service names: `localhost` inside a container would mean that container itself. Never commit root/frontend `.env`, real DSNs, `POSTGRES_PASSWORD`, bootstrap/user passwords, or session/CSRF tokens. `.env.docker.example` contains placeholders only.

**Breaking config migration:** remove `TARGET_WRITE_DATABASE_URL` and merge the old settings into one `TARGET_DATABASE_URL` pointing to a role with the required privileges. A non-empty legacy variable produces an explicit migration error; it is never silently ignored. No separate writer DSN is used anywhere.

### Local Vite development

```powershell
Copy-Item .env.example .env
docker compose -f docker-compose.yml -f docker-compose.demo.yml up -d admin-db target-db
Set-Location backend
go run ./cmd/migrate
# Set secure VELMIR_BOOTSTRAP_ADMIN_LOGIN/PASSWORD before the first bootstrap.
go run ./cmd/bootstrap
go run ./cmd/server
```

In a second terminal:

```powershell
Set-Location frontend
Copy-Item .env.example .env
npm ci
npm run dev
```

Open the Vite URL (normally `http://localhost:5173`). Vite proxies `/api`, `/healthz`, and `/readyz`; cookies remain same-origin. The production Go server serves `dist`, hashed assets with long-term caching, and `index.html` with `no-cache`; `/api/*` never reaches SPA fallback.

**H1 limitations:** external HTTPS/reverse proxy, trusted proxy, CSP/HSTS, Caddy, and backup/restore are outside this stage; there are no automatic target GRANT/DDL statements; field-level permissions are not implemented. **H2:** production TLS/proxy configuration, CSP/HSTS/trusted proxy, Caddy if needed, and verified backup/restore procedures.

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
go run ./cmd/users create -login "viewer@example.test" -display-name "Viewer"
Remove-Item Env:VELMIR_USER_PASSWORD

# Assign a role and list users.
go run ./cmd/users assign-role -login "viewer@example.test" -role viewer
go run ./cmd/users list
```

For a local viewer check, these two commands create the account and assign `viewer`. Sign in using a separate browser profile: schema and records GET are available, while create and edit controls are hidden. Repeating `assign-role` is idempotent; run it again with another role name to assign an additional role. Passwords must contain at least 12 characters, are accepted only through `VELMIR_USER_PASSWORD`, and are stored only as Argon2id hashes. Never put real passwords in documentation or command history.

## Databases and write safety

- **Admin database** (`velmir_admin`) belongs to Velmir. The separate migrate CLI applies Goose migrations only here and creates `users`, `sessions`, `roles`, `user_roles`, `role_permissions`, and `audit_events`.
- **Demo target database** (`velmir_demo`) is a separate PostgreSQL created only by the demo override. Existing initialization fixtures create demo tables, records, a view, PK/FK, and local roles; no later automatic GRANT runs.
- `TARGET_DATABASE_URL` is the single optional DSN. It creates two pools: the read pool always sets `default_transaction_read_only=on`, while the mutation pool is used only for supported INSERT/UPDATE/DELETE after RBAC/CSRF checks.
- `write_enabled` depends on target configuration and effective Velmir permissions. Actual mutation capability is also constrained by PostgreSQL role privileges; viewer remains read-only through Velmir authorization even with a fully privileged DSN.
- Velmir never runs migrations, automatic GRANT, DDL, or arbitrary SQL against a target database. Never point integration tests or a target URL at a real database for local demo setup.

For full CRUD, the target database owner grants the application role only the required privileges in advance: `CONNECT` on the database, `USAGE` on used schemas, `SELECT` for reads, `INSERT`, `UPDATE`, `DELETE` on allowed tables, and `USAGE`, `SELECT`, `UPDATE` on sequences required for default/identity values. A read-only role needs only `CONNECT`, schema `USAGE`, and `SELECT`. The application needs no `CREATE`, `ALTER`, `DROP`, or `TRUNCATE`. Velmir never grants privileges itself.

Without `TARGET_DATABASE_URL`, the backend starts, readiness reports `not_configured`, and `/api/schema` returns `TARGET_NOT_CONFIGURED`. A configured but unavailable target is explicitly `unavailable` and makes `/readyz` fail; `/healthz` remains an independent liveness check.

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

Backend and frontend independently enforce permissions: read only on the parent hides labels/panels; read on parent and child enables them; read only on the referenced resource does not enable resolve for an inaccessible current resource. No session returns 401, while an authenticated user without permission receives 403 without a login redirect. See `docs/api_en.md` for the full batch contract, reverse metadata and permission matrix. Creating child records from a parent form and nested mutations are not implemented.

### Many-to-many editor through junction tables

Parent forms/details generically show target records through a supported PostgreSQL junction table. Detection uses only RBAC-filtered `/api/schema`: exactly two many-to-one FKs to distinct resources and a composite PK consisting exactly of both FKs' columns are required; additional fields must be nullable or have default/generated/identity values. Self-referential relations, surrogate PKs, three or more FKs and required additional junction attributes without default/generated values are unsupported and remain ordinary resources/reverse panels.

The panel requires `read` on parent, target and junction. It loads records through the existing records list with typed parent filters and pagination, and labels through one batch relations/resolve per page. **Add relation** requires a writer, `create` on junction and `read` on target, and uses a server-side-search selector plus ordinary flat junction POST. **Remove relation** requires a writer and `delete` on junction, and uses DELETE with the full composite junction PK. Its confirmation explicitly states that only the junction record is deleted while parent/target records remain unchanged; it is distinct from ordinary **Delete record**. Parent/target `update` grants are not required.

Creating and removing a relation produces ordinary audit `create`/`delete` events only for the junction resource. Duplicate/failed mutations are not audited. After committed `AUDIT_WRITE_FAILED`, the relation mutation is never automatically retried and the list refreshes with a separate warning. The editor does not support nested mutations, related-record creation or editing additional junction attributes. See `docs/api_en.md` for details and the complete permission matrix.

After saving, the current record list is refreshed. Forms warn about unsaved changes before closing or leaving the page. Concurrent updates currently have **last write wins** semantics; optimistic locking and conflict detection are not implemented.

**Delete** is available only with `delete` permission, a configured writer and a table with a PK. The accessible dialog displays the resource and exact simple/composite record identifier, warns about irreversibility and blocks repeat submission. After 204, the list refreshes; deleting the last record on a page navigates to the previous page. Bulk delete is not available: one request deletes exactly one record.

See `docs/api_en.md` for mutation request bodies, record-key encoding and error codes.

## Create, update and delete history

Every successful POST/PATCH/DELETE through Velmir creates an `audit_events` row in the admin database with a UUID, server timestamp, actual actor, `create`/`update`/`delete` action, schema/resource, simple or composite record key and safe before/after JSONB. Create stores after; update stores before and after; delete stores actual before values from `DELETE … RETURNING` and `after_data IS NULL`. Passwords, tokens, secrets and connection strings are omitted. Bigint/numeric remain strings, bytea uses Base64, and JSON/JSONB preserves precision on the backend.

For a row with a primary key, **History** opens a generic panel with the actor, `Intl`-formatted time, action and values. Russian and English switch without reload; pagination and mobile layouts are supported. `GET /api/resources/{schema}/{resource}/records/{key}/history` requires a session and `read`: no session returns 401, while no read grant returns 403 without a login redirect. Deleted-record history remains available by key without checking that the target record still exists. Changes made outside Velmir are not audited.

The target mutation commits in the target database before audit is written to the admin database; there is no cross-database atomicity. If audit fails after commit, the API returns `AUDIT_WRITE_FAILED` with `mutation_committed=true`, and the frontend explains that the change was applied and refreshes data. DELETE has a distinct warning: the record is already deleted, but its history event may not have been recorded. There is no automatic POST/PATCH/DELETE retry or compensating DELETE/UPDATE/INSERT. See `docs/api_en.md` for the full schema, history contract and limitations.

## Demo target persistence

Demo target initialization fixtures run only when a new named volume is created. Use ordinary `stop`, `down`, and repeated `up` to preserve admin/demo data; H1 does not require volume deletion.

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
$env:TEST_TARGET_DATABASE_URL = "postgresql://velmir_writer:velmir_writer_local_only@localhost:5433/velmir_demo?sslmode=disable"
go test -tags=integration ./internal/introspection -v -timeout 30s
go test -tags=integration ./internal/records -v -timeout 30s

$env:TEST_ADMIN_DATABASE_URL = "postgresql://velmir:velmir_local_only@localhost:5432/velmir_admin?sslmode=disable"
go test -tags=integration ./internal/auth -v -timeout 30s
go test -tags=integration ./internal/httpapi -v -timeout 60s

# Full run with both local test URLs already set.
go test -tags=integration ./... -v -timeout 60s
Remove-Item Env:TEST_ADMIN_DATABASE_URL
Remove-Item Env:TEST_TARGET_DATABASE_URL
```

The writer and history integration tests create, update and delete demo records, including simple/composite PKs, FK errors and audit failure after commit. Some audit-failure fixtures intentionally remain in the demo database. The tests refuse non-local hosts and databases other than `velmir_demo`.

The auth, audit storage and admin/history API integration tests require the migrated local `velmir_admin` database, reject non-local hosts and other database names, and roll back their admin transactions. Do not run them against a real admin database. Tests never alter the target schema.

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

### Production without the Caddy TLS proxy

1. Copy `.env.docker.example` to the root `.env`, replace placeholders, set a long random `POSTGRES_PASSWORD`, a matching `ADMIN_DATABASE_URL`, and secure bootstrap credentials. URL-encode passwords inside DSNs.
2. Optionally set the single `TARGET_DATABASE_URL`. Without it, auth/admin UI works, but target resources are not configured.
3. Publish HTTP on loopback:

```bash
docker compose -f docker-compose.yml -f docker-compose.http.yml up --build -d
```

The app is published only as `127.0.0.1:${VELMIR_HTTP_PORT:-8080}`. Use this mode for localhost or an external same-origin HTTPS terminator. Set `TRUSTED_PROXY_CIDRS` only to the actual Docker gateway/proxy CIDR. `APP_ENV=production` requires `SESSION_COOKIE_SECURE=true` and the built frontend; direct HTTP is unsuitable for production login with Secure cookies.

### Production with Caddy HTTPS

1. Point the domain DNS at the server and open inbound TCP 80/443. Set your own values in `.env`, for example:

```dotenv
VELMIR_DOMAIN=admin.example.test
VELMIR_ACME_EMAIL=admin@example.test
VELMIR_DOCKER_SUBNET=172.30.0.0/24
TRUSTED_PROXY_CIDRS=172.30.0.0/24
```

`admin.example.test` is only a placeholder. If the subnet is already allocated, change **both** variables to the same free CIDR. Never use `0.0.0.0/0` or `::/0` as trusted proxies.

2. Start production HTTPS with one command:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml up --build -d
```

Caddy publishes only host ports 80/443, automatically obtains and renews ACME certificates, and redirects HTTP to HTTPS. The app and admin DB publish no host ports; the app accepts internal Docker traffic through Caddy. Caddy forwards Host/protocol and overwrites client forwarding headers. Its admin API listens only on `127.0.0.1:2019` inside the container and is not published. Separate `caddy-data` and `caddy-config` named volumes preserve certificates across restart and reload:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml exec caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
```

The Go server still serves same-origin static/API/SPA routes; Caddy is an additional proxy, not a replacement for the static server.

### Bootstrap and Compose operations

After the first successful production start, remove bootstrap login/password/email from `.env` and the environment, or omit `docker-compose.bootstrap-secrets.yml` on later starts. While a user exists, bootstrap creates nothing and changes no password. An empty production DB still rejects missing, weak, or trivial credentials; `admin/admin` remains limited to the explicit development demo.

Use ordinary `ps`, `logs`, `stop`, `restart`, and `up -d` with the same selected `-f` files. `down` without `-v` preserves named volumes. `admin-db-data`, `admin-db-backups`, demo `target-db-data`, `caddy-data`, and `caddy-config` survive restarts and ordinary `down`. Never use `down -v` as a backup, restore, or upgrade step.

The app runs non-root with a read-only root filesystem, tmpfs `/tmp`, `cap_drop: ALL`, no-new-privileges, a healthcheck, restart policy, and log rotation. Initial limits are app 512 MiB/1 CPU/128 PIDs, admin DB 1 GiB/1 CPU/256 PIDs, and Caddy 256 MiB/0.5 CPU/64 PIDs. These are starting values for validation, not universal sizing recommendations; tune them using observed load.

### Security headers and client IP

The Go app is the sole headers owner; Caddy sets no competing values. Every response receives CSP, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY` plus `frame-ancestors 'none'`, and a restrictive `Permissions-Policy`. Production HTTPS also receives `Strict-Transport-Security: max-age=31536000` without `includeSubDomains` or `preload`; localhost demo sends no HSTS.

CSP allows only `'self'` for scripts/API/fonts and `'self' data:` for images. Scripts have no `unsafe-inline` or `unsafe-eval`, and there are no wildcard sources. `style-src 'self' 'unsafe-inline'` is the only exception because Vue uses runtime inline style attributes for dynamic layouts and controls. Logo, favicon, dialogs, and relation selectors remain same-origin.

By default `TRUSTED_PROXY_CIDRS` is empty and `X-Forwarded-For`/`X-Real-IP` are ignored. Headers are used only when the immediate TCP peer belongs to a trusted CIDR; the chain is evaluated right to left so forged leftmost entries cannot change the client IP used by login rate limiting. `X-Forwarded-Host` is never used for redirects. In TLS Compose the app has no host port, so an external client cannot bypass Caddy to spoof the IP.

### Complete environment table

The root `.env` is used by Docker Compose and local Go CLIs through `godotenv`; `frontend/.env` is only for Vite. **Prod** is the main Compose stack, **TLS** is the Caddy override, and **demo** is the demo override. For `_FILE` variables, the file content is secret rather than its path.

| Variable | Purpose | Required | Default | Mode | Secret | Safe example |
|---|---|---|---|---|---|---|
| `POSTGRES_DB` | Admin DB name | prod: yes; demo: fixed | none | prod/demo | no | `velmir_admin` |
| `POSTGRES_USER` | Admin DB role | prod: yes; demo: fixed | none | prod/demo | no | `velmir` |
| `POSTGRES_PASSWORD` | Admin DB password | env prod: yes | none | prod/demo | **yes** | `<long-random-password>` |
| `POSTGRES_PASSWORD_FILE` | Native PostgreSQL secret file | Instead of password in secrets mode | empty | prod | path no; file **yes** | `/run/secrets/postgres_password` |
| `ADMIN_DATABASE_URL` | Admin DSN | prod/dev: yes; demo: fixed | none | all | **yes** | `postgresql://velmir:<password>@admin-db:5432/velmir_admin?sslmode=disable` |
| `ADMIN_DATABASE_URL_FILE` | File containing admin DSN | Instead of env in secrets mode | empty | prod/dev | path no; file **yes** | `/run/secrets/admin_database_url` |
| `TARGET_DATABASE_URL` | Single DSN for read/mutation pools | no | empty | all | **yes** | `postgresql://app:<password>@target-db:5432/app?sslmode=require` |
| `TARGET_DATABASE_URL_FILE` | File containing the single target DSN | no; instead of env | empty | prod/dev | path no; file **yes** | `/run/secrets/target_database_url` |
| `VELMIR_DEPRECATED_TARGET_WRITE_DATABASE_URL` | Legacy migration error; not a pool | do not set | empty | compatibility | **yes** | empty |
| `APP_ENV` | Security mode | no | backend `development`; Compose `production` | all | no | `production` |
| `SESSION_COOKIE_SECURE` | Secure cookie flag | prod: `true` | backend `false`; Compose `true` | all | no | `true` |
| `VELMIR_BOOTSTRAP_ADMIN_LOGIN` | First admin login | Empty prod DB only | empty; demo fallback | all | no | `admin@example.test` |
| `VELMIR_BOOTSTRAP_ADMIN_LOGIN_FILE` | File containing first admin login | Instead of env | empty | prod/dev | no | `/run/secrets/bootstrap_admin_login` |
| `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` | First admin password | Empty prod DB only | empty; demo fallback | all | **yes** | `<long-random-password>` |
| `VELMIR_BOOTSTRAP_ADMIN_PASSWORD_FILE` | File containing first admin password | Instead of env | empty | prod/dev | path no; file **yes** | `/run/secrets/bootstrap_admin_password` |
| `VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME` | First admin display name | no | `Administrator` | all | no | `Administrator` |
| `VELMIR_BOOTSTRAP_ADMIN_EMAIL` / `_FILE` | Legacy login fallback | no | empty | all | no | `admin@example.test` |
| `VELMIR_DEMO_MODE` | Demo-only bootstrap | Demo only | `false` | demo | no | `true` |
| `VELMIR_HTTP_PORT` | Non-TLS host HTTP port | no | `8080` | HTTP/demo | no | `8080` |
| `VELMIR_PROXY_HTTP_PORT` | Caddy host HTTP port | no | `80` | TLS | no | `80` |
| `VELMIR_PROXY_HTTPS_PORT` | Caddy host HTTPS port | no | `443` | TLS | no | `443` |
| `VELMIR_DOMAIN` | Public Caddy site name | TLS: yes | none | TLS | no | `admin.example.test` |
| `VELMIR_ACME_EMAIL` | ACME account email | TLS: yes | none | TLS | no | `admin@example.test` |
| `VELMIR_DOCKER_SUBNET` | TLS Compose network subnet | no | `172.30.0.0/24` | TLS | no | `172.30.0.0/24` |
| `TRUSTED_PROXY_CIDRS` | Trusted immediate proxies | TLS: yes; otherwise no | empty | prod/dev/TLS | no | `172.30.0.0/24` |
| `VELMIR_TARGET_PORT` | Demo target host port | no | `5433` | demo | no | `5433` |
| `VELMIR_IMAGE` | Application image | no | `velmir:local` | prod/demo/TLS | no | `velmir:local` |
| `HTTP_ADDR` | Backend listen address | no | `127.0.0.1:8080`; Compose `0.0.0.0:8080` | dev; Compose fixed | no | `127.0.0.1:8080` |
| `FRONTEND_DIST_PATH` | Built Vue directory | prod: yes | image `/app/frontend/dist` | all | no | `/app/frontend/dist` |
| `LOG_LEVEL` | Structured log level | no | `info` | all | no | `info` |
| `SESSION_IDLE_TIMEOUT` | Idle session lifetime | no | `30m` | all | no | `30m` |
| `SESSION_ABSOLUTE_TIMEOUT` | Absolute session lifetime | no | `24h`, at least idle | all | no | `24h` |
| `SESSION_CLEANUP_INTERVAL` | Cleanup period | no | `15m` | all | no | `15m` |
| `SESSION_CLEANUP_BATCH_SIZE` | Maximum rows per cleanup batch | no | `1000` | all | no | `1000` |
| `DATABASE_CONNECT_TIMEOUT` | DB connect timeout | no | `5s` | all | no | `5s` |
| `ADMIN_DATABASE_MAX_CONNS` | Admin pool max connections | no | `10` | all | no | `10` |
| `TARGET_DATABASE_MAX_CONNS` | Max connections in each target pool | no | `10` | all | no | `10` |
| `DATABASE_MAX_CONN_LIFETIME` | Connection lifetime | no | `30m` | all | no | `30m` |
| `DATABASE_MAX_CONN_IDLE_TIME` | Idle connection time | no | `5m` | all | no | `5m` |
| `HTTP_READ_HEADER_TIMEOUT` | Read headers timeout | no | `5s` | all | no | `5s` |
| `HTTP_READ_TIMEOUT` | Read timeout | no | `15s` | all | no | `15s` |
| `HTTP_WRITE_TIMEOUT` | Write timeout | no | `30s` | all | no | `30s` |
| `HTTP_IDLE_TIMEOUT` | Keep-alive idle timeout | no | `60s` | all | no | `60s` |
| `HTTP_SHUTDOWN_TIMEOUT` | Graceful SIGTERM shutdown | no | `10s` | all | no | `10s` |
| `HTTP_MAX_JSON_BODY_SIZE` | JSON body limit in bytes | no | `1048576` | all | no | `1048576` |

For `cmd/users`, the password is accepted only through secret `VELMIR_USER_PASSWORD` (12–1024 characters), never a CLI argument. Vite development uses non-secret `VITE_BACKEND_BASE_URL=` and `VITE_BACKEND_PROXY_TARGET=http://127.0.0.1:8080`. The production bundle receives no DSNs/passwords and uses the same-origin API.

A non-empty env value and its corresponding `_FILE` are mutually exclusive. Empty or unreadable files produce a config error without exposing content; the DSN uses file content rather than its path. Only trailing CR/LF line endings are removed. Never commit `.env`, real DSNs, passwords, session/CSRF tokens, or backups.

**Breaking config migration:** remove `TARGET_WRITE_DATABASE_URL`; no separate writer DSN is used. The single `TARGET_DATABASE_URL` is preserved in every mode.

### Docker secrets

`docker-compose.secrets.yml` mounts the PostgreSQL password into admin-db and the admin DSN only into migrate/bootstrap/app. `docker-compose.target-secret.yml` separately adds the target DSN only to app. `docker-compose.bootstrap-secrets.yml` is needed only for the first administrator. Secrets are not copied into the image or frontend bundle.

Create local files without putting real values in Git:

```powershell
New-Item -ItemType Directory -Path secrets -Force
# Write each secret with an editor rather than on the command line:
notepad secrets\postgres_password
notepad secrets\admin_database_url
notepad secrets\bootstrap_admin_login
notepad secrets\bootstrap_admin_password
# Optional: secrets\target_database_url
```

```bash
mkdir -p secrets
chmod 700 secrets
# Create the files with an editor, then:
chmod 600 secrets/*
```

The `secrets/`, `backups/`, and `*.dump` paths are excluded from Git and the Docker build context. Example first production secrets + HTTPS start:

```bash
docker compose -f docker-compose.yml -f docker-compose.secrets.yml -f docker-compose.bootstrap-secrets.yml -f docker-compose.target-secret.yml -f docker-compose.tls.yml up --build -d
```

Without a target or on later starts, omit the corresponding override. For non-TLS, replace the TLS file with `docker-compose.http.yml`. Ordinary env-based local development is unchanged.

### Admin database backup

Backup uses `pg_dump` custom format inside admin-db and the separate `admin-db-backups` named volume; no password is passed as an argument and binary output never crosses shell/PowerShell redirection. The full admin backup includes users, roles/permissions, sessions, settings (if introduced), audit events, and `goose_db_version`. It does **not** include the target DB; its owner is responsible for target backups.

On the running stack, with the desired `COMPOSE_FILE` or current base Compose selection:

```bash
sh scripts/backup-admin.sh
# Or an explicit name:
sh scripts/backup-admin.sh velmir-admin-before-upgrade.dump
docker compose exec -T admin-db ls -l /backups
```

```powershell
.\scripts\Backup-Admin.ps1
# Or:
.\scripts\Backup-Admin.ps1 -BackupName velmir-admin-before-upgrade.dump
docker compose exec -T admin-db ls -l /backups
```

For demo in PowerShell, first set `$env:COMPOSE_FILE = "docker-compose.yml;docker-compose.demo.yml"`; in shell use `COMPOSE_FILE=docker-compose.yml:docker-compose.demo.yml`. Never redirect `.dump` through `>`: PowerShell redirection can corrupt binary data. Use `docker cp` for external archiving rather than text redirection. Backup files never enter Git or the image.

### Restore and verification

Prefer a fresh isolated PostgreSQL/admin DB and a separate Compose project. Do not restore over the production DB. The scripts below create a **new** DB with `createdb` (an existing name fails), then run `pg_restore --single-transaction --exit-on-error`; there is no automatic destructive restore.

```bash
sh scripts/restore-admin-fresh.sh velmir-admin-before-upgrade.dump velmir_admin_restored
```

```powershell
.\scripts\Restore-AdminFresh.ps1 -BackupName velmir-admin-before-upgrade.dump -NewDatabaseName velmir_admin_restored
```

For full isolation, start a temporary admin-db with separate project/volumes and mount a backup copy at `/backups`; run the same restore there. Then point a separate app at the restored `ADMIN_DATABASE_URL`, run idempotent migrate, and verify the existing admin login, `/api/auth/me`, roles/permissions, and history. `TARGET_DATABASE_URL` is not required to verify restored admin state. SQL checks should confirm users, `user_roles`, `role_permissions`, `audit_events`, `sessions`, and the latest `goose_db_version` match the backup. Stop only temporary resources after verification; do not delete existing project volumes.

### Upgrade and rollback

1. Create and verify a non-empty backup before upgrading.
2. Update the image and run the separate migrate job with the admin DB available:

```bash
docker compose -f docker-compose.yml -f docker-compose.http.yml up -d admin-db
docker compose -f docker-compose.yml -f docker-compose.http.yml run --rm migrate
docker compose -f docker-compose.yml -f docker-compose.http.yml run --rm migrate
docker compose -f docker-compose.yml -f docker-compose.http.yml up -d
```

Repeated migrate is safe because of Goose state. For TLS/secrets, use the same complete `-f` set. Rolling back the image **does not roll back migrations**: start an old image only if it is compatible with the newer schema. There is no automatic destructive migration rollback; restoring a backup is a separate manual disaster recovery operation into a fresh DB, with potential loss of changes made after the backup.

### Multiple instances

Expired session cleanup is safe on multiple replicas because of batches and `SKIP LOCKED`; it touches only the admin DB and never deletes active sessions. The login rate limiter remains **process-local** and is **not synchronized** between replicas: the effective total limit depends on request distribution. Use a separate proxy limit if a global limit is required. Sticky sessions are unnecessary because session records are shared; background jobs are not a distributed scheduler.

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

**H2 hardening:** optional Caddy HTTPS, explicit trusted proxy configuration, CSP/HSTS, Docker secrets, session maintenance, and admin backup/restore are included. There are no automatic target GRANT/DDL statements. Stage 8A adds field-level permissions for create/update only; field-level read filtering is not implemented.

## Interface design and language

- The corporate palette consists of Velmir Deep `#0F6B5F`, Midnight Bark `#101B20`, Source Mist `#F4F7F5` and Mimir Gold `#C18A3D`. Semantic color tokens, including contrast variants for dark surfaces and error/success/disabled states, are centralized in `frontend/src/style.css`.
- Source SVG and PNG assets are stored in `frontend/public/brand/`. The full logo appears on the login page, the compact logo in the sidebar, and the mark on narrow screens; light/dark variants match the surface. CSS does not alter the brand mark's shape.
- SVG/PNG favicons, the Apple touch icon and `site.webmanifest` are linked in `frontend/index.html`. No service worker is used.
- The interface is available in Russian and English. Russian is the default; the shared footer switch changes the language without a reload and updates `html lang`. The explicit choice is stored only in `localStorage` under `velmir_locale` (`ru` or `en`) and requires no backend API. Namespaced translations live in `frontend/src/locales/`, with English as the fallback for missing strings.

## Authentication and sessions

The application calls `GET /api/auth/me` at startup, redirects anonymous users to `/login`, and restores the original route after login. Logout revokes only the current session. Record mutations and logout require the server-verified `X-CSRF-Token` header. Passwords, session tokens and CSRF tokens are not stored in `localStorage`; the session cookie is HttpOnly and the CSRF token remains in memory.

Session cookies use `SameSite=Lax`; idle expiration defaults to `30m` and absolute expiration to `24h`. Expired records are deleted in background batches every `15m` (configurable); active sessions are preserved, and SIGTERM stops cleanup. See `docs/api_en.md` for cookie attributes, configuration and endpoint responses.

## Roles, permissions and user management

Access is denied by default. Roles can be combined: a user's effective permissions are the union of every assigned role. The built-in `admin` role allows reading, creating, updating and deleting every resource; `viewer` allows reading only. `read`, `create`, `update` and `delete` grants can target a specific schema and resource. The `delete` grant is independent of other grants and writer availability.

The bootstrap administrator receives `admin` automatically. A new user receives no role by default and can access only `/api/auth/me` with an empty permissions list; protected resources return 403. The backend filters `/api/schema` by `read`, and the frontend hides unavailable resources and create/update/delete controls. Viewers see an explicit read-only state.

Administrators have a separate **Administration** section for creating users, changing display names, enabling/disabling accounts, resetting passwords and assigning roles, plus creating and editing custom roles and permissions. The section is hidden from viewers and users without roles, while the backend independently checks the `admin` system role, session and CSRF. Disabling an account and resetting a password revoke all user sessions. Self-disable, removing your own `admin` role and removing admin rights from the last active administrator are forbidden. System roles are immutable; wildcard `*.*` and concrete schema/resource grants support `read`, `create`, `update` and `delete`. A custom role receives `delete` only after an explicit assignment in PermissionEditor; the migration does not grant it to existing custom roles. Changes apply to subsequent requests without a restart. See `docs/api_en.md` for complete API contracts.

### Field permissions for create/update

For custom roles with concrete schema/resource grants, PermissionEditor supports independent field settings for `create` and `update`: **All available fields** (`unrestricted`) or **Selected fields** (`allowlist`). An empty allowlist is a distinct explicit **No available fields** state, not unrestricted. Generated, identity, PK and read-only columns cannot be added to an allowlist. Wildcard `*.*` is always unrestricted and offers no field restrictions. System roles remain immutable: `admin` is unrestricted and `viewer` has no create/update.

The admin database migration preserves existing role capabilities as unrestricted, and legacy API payloads without field configuration also mean unrestricted. When roles are unioned, one unrestricted grant for a resource/action makes effective access unrestricted; otherwise allowlists are unioned. An empty allowlist adds no fields and does not revoke other roles' grants. Changes apply to subsequent requests without a restart.

`/api/schema` returns backend-computed `creatable_fields`, `updatable_fields`, `can_create`, `can_update` and field-related disabled reasons. The frontend uses them for controls, payloads, FK selectors and junction creation, while the backend independently rejects forbidden and mixed POST/PATCH requests before mutation and audit. If a required NOT NULL field without a default/generated value is unavailable, create is not offered and returns a safe structured 403. There is no field-level read filtering: list, history and related-label read visibility is unchanged.

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

- **Admin database** (`velmir_admin`) belongs to Velmir. The separate migrate CLI applies Goose migrations only here and creates `users`, `sessions`, `roles`, `user_roles`, `role_permissions`, `role_field_permissions`, and `audit_events`.
- **Demo target database** (`velmir_demo`) is a separate PostgreSQL created only by the demo override. Existing initialization fixtures create demo tables, records, a view, PK/FK, and local roles; no later automatic GRANT runs.
- `TARGET_DATABASE_URL` is the single optional DSN. It creates two pools: the read pool always sets `default_transaction_read_only=on`, while the mutation pool is used only for supported INSERT/UPDATE/DELETE after resource/field RBAC and CSRF checks. No separate writer DSN exists.
- `write_enabled` depends on target configuration, action permissions and actual write capability using effective fields. Actual mutation capability is also constrained by PostgreSQL role privileges; viewer remains read-only through Velmir authorization even with a fully privileged DSN.
- Velmir never runs migrations, automatic GRANT, DDL, or arbitrary SQL against a target database. Never point integration tests or a target URL at a real database for local demo setup.

For full CRUD, the target database owner grants the application role only the required privileges in advance: `CONNECT` on the database, `USAGE` on used schemas, `SELECT` for reads, `INSERT`, `UPDATE`, `DELETE` on allowed tables, and `USAGE`, `SELECT`, `UPDATE` on sequences required for default/identity values. A read-only role needs only `CONNECT`, schema `USAGE`, and `SELECT`. The application needs no `CREATE`, `ALTER`, `DROP`, or `TRUNCATE`. Velmir never grants privileges itself.

Without `TARGET_DATABASE_URL`, the backend starts, readiness reports `not_configured`, and `/api/schema` returns `TARGET_NOT_CONFIGURED`. A configured but unavailable target is explicitly `unavailable` and makes `/readyz` fail; `/healthz` remains an independent liveness check.

## Creating, editing and deleting records

The frontend builds all forms from `/api/schema`; there are no table-specific forms. Supported inputs cover text, boolean, integer, bigint/numeric, date, timestamps, UUID, enums and JSON. Nullable fields have an explicit NULL control, and create forms can explicitly use a database default. Generated and `always` identity fields are not required or editable.

Create/update forms use only effective `creatable_fields`/`updatable_fields`: unauthorized controls are hidden, their stale values are omitted from payloads, and required validation applies only to accessible fields. An empty update set disables editing without changing read-only details. An unavailable FK hides its many-to-one selector. Switching ru/en preserves form values.

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

The panel requires `read` on parent, target and junction. It loads records through the existing records list with typed parent filters and pagination, and labels through one batch relations/resolve per page. **Add relation** requires a writer, `create` on junction, every required junction FK column in `creatable_fields`, and `read` on target; it uses a server-side-search selector plus ordinary flat junction POST. **Remove relation** requires a writer and `delete` on junction, and uses DELETE with the full composite junction PK. Its confirmation explicitly states that only the junction record is deleted while parent/target records remain unchanged; it is distinct from ordinary **Delete record**. Parent/target `update` grants are not required.

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

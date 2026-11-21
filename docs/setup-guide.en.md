# Running Velmir

[Русская версия](setup-guide.ru.md)

## Purpose and startup modes

Velmir is a metadata-driven PostgreSQL admin panel. It uses a separate admin database for users, sessions, roles, permissions, and change history, and never runs migrations, DDL, or automatic GRANT statements in the target database.

The following Docker modes are supported:

- **Local demo**: an admin DB and an isolated demo target DB, localhost HTTP, and an automatic demo administrator.
- **Production HTTP behind an external HTTPS terminator**: the application listens only on loopback while an external proxy provides HTTPS.
- **Production Caddy HTTPS**: Caddy publishes HTTP/HTTPS, obtains certificates, and proxies requests to the internal application.
- **Docker secrets**: DSNs, the PostgreSQL password, and bootstrap credentials are supplied through files instead of environment values; secrets can be combined with HTTP or Caddy HTTPS.

See the [API documentation](api_en.md) for endpoints and permissions. A short project overview is available in the [README](../README_EN.md).

## System requirements

- Windows or macOS: Docker Desktop with Docker Compose v2.
- Linux: Docker Engine with the Docker Compose v2 plugin.
- Free ports: demo `8080` and `5433`; the selected `VELMIR_HTTP_PORT` for production HTTP; `80` and `443` or the selected proxy ports for Caddy.
- Access to Docker Hub images for the first build.
- For Caddy with public ACME: a registered domain, DNS, and inbound TCP 80/443.

Go, Node.js, npm, and Goose are **not required** on the host for Docker startup: the frontend, backend, and migrate CLI are built in the Docker image. They are needed only for local development and source-based checks.

## Quick local demo startup

Run this single command from the repository root in either shell or PowerShell:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml up --build -d
```

Open `http://127.0.0.1:8080` and sign in with login `admin` and password `admin`. The demo override creates an admin DB and a `velmir_demo` target DB containing demonstration tables and relations. The app is published at `127.0.0.1:8080` and the demo target at `127.0.0.1:5433`; the admin DB is not published.

**`admin/admin` is an unsafe credential pair for the local demo only. Do not expose the demo to a network or reuse these credentials in production.**

Check status and logs:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml ps
docker compose -f docker-compose.yml -f docker-compose.demo.yml logs --tail=100
docker compose -f docker-compose.yml -f docker-compose.demo.yml logs -f app
```

Stop without deleting named volumes:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml stop
```

Start again:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml up -d
```

## Production preparation

Create the root `.env` from the safe example:

```bash
cp .env.docker.example .env
```

```powershell
Copy-Item .env.docker.example .env
```

Replace the placeholders with your own values. Do not use the placeholders below as working passwords:

```dotenv
POSTGRES_DB=velmir_admin
POSTGRES_USER=velmir
POSTGRES_PASSWORD=<long-random-database-password>
ADMIN_DATABASE_URL=postgresql://velmir:<url-encoded-database-password>@admin-db:5432/velmir_admin?sslmode=disable
APP_ENV=production
SESSION_COOKIE_SECURE=true
VELMIR_HTTP_PORT=8080
VELMIR_BOOTSTRAP_ADMIN_LOGIN=<unique-admin-login>
VELMIR_BOOTSTRAP_ADMIN_PASSWORD=<long-random-admin-password>
VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME=Administrator
# Optional:
TARGET_DATABASE_URL=postgresql://<target-user>:<url-encoded-target-password>@<target-host>:5432/<target-database>?sslmode=require
```

- `POSTGRES_PASSWORD` and the password inside `ADMIN_DATABASE_URL` must match; URL-encode special characters in the DSN.
- Inside Compose the admin host is `admin-db`, not `localhost`.
- `TARGET_DATABASE_URL` is optional: authentication and the administration UI work without a target.
- Production requires `APP_ENV=production`, `SESSION_COOKIE_SECURE=true`, and the compiled frontend; the production Docker image already contains it.
- Never commit `.env`, real DSNs, passwords, tokens, backups, or the `secrets/` directory to Git. They are excluded through `.gitignore`; do not remove those rules.

When using secrets, leave the corresponding secret values in `.env` empty and follow the [Docker secrets](#docker-secrets) section.

## First administrator

The dependency order is `admin-db` healthy → `migrate` completed successfully → `bootstrap` completed successfully → `app`. The current admin schema version is **Goose 9**.

Bootstrap creates an administrator only when the `users` table is empty and automatically assigns the `admin` system role:

- login is required, contains no whitespace, and is at most 254 characters;
- password is required, contains 12–1024 characters, and must be long and random;
- display name is optional, defaults to `Administrator`, and is limited to 200 characters;
- production rejects missing/weak credentials and a list of trivial passwords; `admin/admin` is forbidden;
- `admin/admin` is created only by the explicit demo override with `APP_ENV=development` and `VELMIR_DEMO_MODE=true`.

After the first successful startup, remove `VELMIR_BOOTSTRAP_ADMIN_LOGIN`, legacy `VELMIR_BOOTSTRAP_ADMIN_EMAIL`, and `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` from `.env` and the environment. In secrets mode, stop including `docker-compose.bootstrap-secrets.yml`.

Bootstrap runs once: when at least one user exists, it creates nothing and **does not reset the password**. A later startup without bootstrap credentials is safe and preserves the existing administrator login and password.

## Target database

`TARGET_DATABASE_URL` is the single DSN for both the read pool and the mutation pool. No separate writer DSN is supported. The read pool always sets `default_transaction_read_only=on`; the mutation pool is used only for supported INSERT/UPDATE/DELETE after session, CSRF, resource RBAC, and field-level checks.

The target database owner grants the application role only the required privileges in advance:

- `CONNECT` on the database;
- `USAGE` on the schemas in use;
- `SELECT` for reads;
- `INSERT`, `UPDATE`, and `DELETE` only on tables allowed for mutations;
- `USAGE`, `SELECT`, and `UPDATE` on sequences required for default/identity values.

A read-only role needs only `CONNECT`, schema `USAGE`, and `SELECT`. Velmir runs no automatic DDL, migrations, or GRANT statements in the target. The application needs no `CREATE`, `ALTER`, `DROP`, or `TRUNCATE` privileges.

The target is optional. Without a DSN, `/readyz` reports `target_database=not_configured` and `/api/schema` returns `TARGET_NOT_CONFIGURED`. An unavailable configured target reports `unavailable` and makes `/readyz` fail; `/healthz` remains an independent liveness check.

PostgreSQL privileges do not replace Velmir RBAC: an authenticated user also needs the resource/action permission, while field permissions restrict read/create/update at column level.

Inside Compose, use DNS service names such as `admin-db` or your `target-db`, not `localhost`: `localhost` inside a container refers to that container. For an external target DB, use a hostname/IP reachable from the container and an appropriate `sslmode`.

## Production HTTP behind an external proxy

Configure `.env`, then run:

```bash
docker compose -f docker-compose.yml -f docker-compose.http.yml up --build -d
```

The app is published only as `127.0.0.1:${VELMIR_HTTP_PORT:-8080}`. The admin DB has no host port. This mode is intended for an external same-origin HTTPS terminator on the same host; do not expose the app directly as production HTTP.

- Keep `SESSION_COOKIE_SECURE=true`. Browsers do not send Secure session cookies over direct HTTP, so production login must go through the HTTPS proxy.
- The proxy must preserve the original Host, send the correct `X-Forwarded-Proto`, and replace client forwarding headers instead of trusting client-provided values.
- Set `TRUSTED_PROXY_CIDRS` to a comma-separated list containing only the actual Docker gateway/proxy CIDRs. Never use `0.0.0.0/0` or `::/0`.
- Checks: `docker compose -f docker-compose.yml -f docker-compose.http.yml ps` and `... logs app`.

## Production Caddy HTTPS

1. Point the domain's DNS A and/or AAAA records at the server and open inbound TCP 80/443.
2. Set your own values:

```dotenv
VELMIR_DOMAIN=admin.example.test
VELMIR_ACME_EMAIL=admin@example.test
VELMIR_DOCKER_SUBNET=172.30.0.0/24
TRUSTED_PROXY_CIDRS=172.30.0.0/24
```

`admin.example.test` is only a placeholder. If the subnet is allocated, change **both** CIDR variables to the same free subnet. For non-standard ports, use `VELMIR_PROXY_HTTP_PORT` and `VELMIR_PROXY_HTTPS_PORT`.

3. Start:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml up --build -d
```

Caddy publishes only proxy ports 80/443, redirects HTTP → HTTPS, and proxies requests to `app:8080`. The app and admin DB have no host ports. Certificates and the ACME account persist in the `caddy-data` and `caddy-config` named volumes.

Validate and reload the configuration:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml exec caddy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker compose -f docker-compose.yml -f docker-compose.tls.yml exec caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
```

Local `caddy validate` and an internal HTTPS smoke do not prove that a public ACME certificate can be issued. Public ACME is verified only with a real DNS domain and ports reachable from the internet; inspect `logs caddy` and the browser certificate.

## Docker secrets

Supported backend `_FILE` variables are:

- `ADMIN_DATABASE_URL_FILE`;
- `TARGET_DATABASE_URL_FILE`;
- `VELMIR_BOOTSTRAP_ADMIN_LOGIN_FILE`;
- `VELMIR_BOOTSTRAP_ADMIN_PASSWORD_FILE`;
- legacy `VELMIR_BOOTSTRAP_ADMIN_EMAIL_FILE` as a login fallback when using a custom secrets mount.

The official PostgreSQL image supports `POSTGRES_PASSWORD_FILE`. The Compose secrets overrides mount the first four primary variables; the legacy email file requires custom configuration. Display name has no `_FILE` variant.

A non-empty env value and its corresponding `_FILE` are mutually exclusive. The file must exist, be readable, and contain a non-empty value; trailing CR/LF is removed. The DSN uses the file content, not its path. Empty or unreadable files produce a config error without exposing content.

Create secret files with an editor rather than putting secrets into command history:

```bash
mkdir -p secrets
chmod 700 secrets
# Create each file with an editor, then:
chmod 600 secrets/*
```

```powershell
New-Item -ItemType Directory -Path secrets -Force
notepad secrets\postgres_password
notepad secrets\admin_database_url
notepad secrets\bootstrap_admin_login
notepad secrets\bootstrap_admin_password
# Optional: notepad secrets\target_database_url
```

Restrict directory access using OS permissions. `secrets/`, `*.dump`, and `backups/` are excluded from Git and the Docker build context. Do not print the files in a terminal or attach them to support requests.

First production secrets + Caddy HTTPS startup in shell or PowerShell:

```bash
docker compose -f docker-compose.yml -f docker-compose.secrets.yml -f docker-compose.bootstrap-secrets.yml -f docker-compose.target-secret.yml -f docker-compose.tls.yml up --build -d
```

Without a target, omit `docker-compose.target-secret.yml`. For production HTTP, replace the TLS override:

```bash
docker compose -f docker-compose.yml -f docker-compose.secrets.yml -f docker-compose.bootstrap-secrets.yml -f docker-compose.http.yml up --build -d
```

After creating the administrator, restart without `docker-compose.bootstrap-secrets.yml`. Secret file paths are not secrets; their content is. Secrets are not copied into the image or frontend bundle.

## Complete environment variables table

**Prod** means base/HTTP/TLS production Compose, **demo** means the demo override, and **dev** means running backend/Vite outside Docker. Backend and Compose defaults are distinguished where necessary. For `_FILE`, the file content is secret.

| Variable | Required | Default | Allowed values | Mode | Secret | Purpose |
|---|---|---|---|---|---|---|
| `POSTGRES_DB` | prod: yes; demo: fixed | none | PostgreSQL DB name | prod/demo | no | Admin DB name |
| `POSTGRES_USER` | prod: yes; demo: fixed | none | PostgreSQL role | prod/demo | no | Admin DB role |
| `POSTGRES_PASSWORD` | env prod: yes | none | non-empty password | prod/demo | **yes** | Admin DB password |
| `POSTGRES_PASSWORD_FILE` | secrets: instead of env | empty | readable file path | prod | content **yes** | Native PostgreSQL password file |
| `ADMIN_DATABASE_URL` | prod/dev: yes; demo: fixed | none | PostgreSQL DSN | all | **yes** | Admin DSN for migrate/bootstrap/app |
| `ADMIN_DATABASE_URL_FILE` | instead of env in secrets | empty | readable file path | prod/dev | content **yes** | File containing admin DSN |
| `TARGET_DATABASE_URL` | no | empty | PostgreSQL DSN or empty | all | **yes** | Single DSN for read/mutation pools |
| `TARGET_DATABASE_URL_FILE` | no; instead of env | empty | readable file path | prod/dev | content **yes** | File containing target DSN |
| `TARGET_WRITE_DATABASE_URL` | do not set | empty | legacy rejection only | migration | **yes** | Removed writer DSN; remove it and use `TARGET_DATABASE_URL` |
| `VELMIR_DEPRECATED_TARGET_WRITE_DATABASE_URL` | do not set | empty | Compose legacy mapping | compatibility | **yes** | Triggers the same explicit legacy rejection |
| `APP_ENV` | no | backend `development`; Compose `production` | `development`, `production` | all | no | Security mode |
| `SESSION_COOKIE_SECURE` | prod: `true` | backend `false`; Compose `true` | `true`, `false` | all | no | Secure flag for authentication cookies |
| `VELMIR_BOOTSTRAP_ADMIN_LOGIN` | empty prod DB: yes | empty; demo fallback | ≤254, no whitespace | all | no | First administrator login |
| `VELMIR_BOOTSTRAP_ADMIN_LOGIN_FILE` | instead of login env | empty | readable file path | prod/dev | no | File containing first administrator login |
| `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` | empty prod DB: yes | empty; demo fallback | 12–1024, non-trivial | all | **yes** | First administrator password |
| `VELMIR_BOOTSTRAP_ADMIN_PASSWORD_FILE` | instead of password env | empty | readable file path | prod/dev | content **yes** | File containing first administrator password |
| `VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME` | no | `Administrator` | ≤200 | all | no | First administrator display name |
| `VELMIR_BOOTSTRAP_ADMIN_EMAIL` | no | empty | legacy login fallback | all | no | Legacy login; prefer `..._LOGIN` |
| `VELMIR_BOOTSTRAP_ADMIN_EMAIL_FILE` | no | empty | readable file path | prod/dev | no | Legacy login from a custom file mount |
| `VELMIR_DEMO_MODE` | demo: `true`; prod: `false` | `false` | `true`, `false` | demo/prod | no | Enables demo-only bootstrap |
| `VELMIR_HTTP_PORT` | no | `8080` | TCP port | HTTP/demo | no | Loopback app host port |
| `VELMIR_PROXY_HTTP_PORT` | no | `80` | TCP port | TLS | no | Caddy host/container HTTP port |
| `VELMIR_PROXY_HTTPS_PORT` | no | `443` | TCP port | TLS | no | Caddy host/container HTTPS port |
| `VELMIR_DOMAIN` | TLS: yes | none | DNS hostname | TLS | no | Public Caddy site name |
| `VELMIR_ACME_EMAIL` | TLS: yes | none | email | TLS | no | ACME account email |
| `VELMIR_DOCKER_SUBNET` | no | `172.30.0.0/24` | free CIDR | TLS | no | TLS Compose network subnet |
| `TRUSTED_PROXY_CIDRS` | TLS: yes; otherwise optional | empty | comma-separated CIDRs | prod/dev/TLS | no | Trusted immediate proxy networks |
| `VELMIR_TARGET_PORT` | no | `5433` | TCP port | demo | no | Demo target loopback host port |
| `VELMIR_IMAGE` | no | `velmir:local` | Docker image reference | prod/demo/TLS | no | Application image |
| `HTTP_ADDR` | no | backend `127.0.0.1:8080`; Compose `0.0.0.0:8080` | host:port | dev; Compose fixed | no | Backend listen address |
| `FRONTEND_DIST_PATH` | production outside image: yes | backend empty; image `/app/frontend/dist` | existing dist directory | all | no | Built Vue directory |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` | all | no | Structured log level |
| `SESSION_IDLE_TIMEOUT` | no | `30m` | positive Go duration | all | no | Idle session lifetime |
| `SESSION_ABSOLUTE_TIMEOUT` | no | `24h` | Go duration ≥ idle | all | no | Absolute session lifetime |
| `SESSION_CLEANUP_INTERVAL` | no | `15m` | positive Go duration | all | no | Expired-session cleanup period |
| `SESSION_CLEANUP_BATCH_SIZE` | no | `1000` | positive integer | all | no | Maximum rows per cleanup batch |
| `DATABASE_CONNECT_TIMEOUT` | no | `5s` | positive Go duration | all | no | DB connect timeout |
| `ADMIN_DATABASE_MAX_CONNS` | no | `10` | positive integer | all | no | Admin pool max connections |
| `TARGET_DATABASE_MAX_CONNS` | no | `10` | positive integer | all | no | Max connections in each target pool |
| `DATABASE_MAX_CONN_LIFETIME` | no | `30m` | positive Go duration | all | no | Connection lifetime |
| `DATABASE_MAX_CONN_IDLE_TIME` | no | `5m` | positive Go duration | all | no | Idle connection time |
| `HTTP_READ_HEADER_TIMEOUT` | no | `5s` | positive Go duration | all | no | Read headers timeout |
| `HTTP_READ_TIMEOUT` | no | `15s` | positive Go duration | all | no | Request read timeout |
| `HTTP_WRITE_TIMEOUT` | no | `30s` | positive Go duration | all | no | Response write timeout |
| `HTTP_IDLE_TIMEOUT` | no | `60s` | positive Go duration | all | no | Keep-alive idle timeout |
| `HTTP_SHUTDOWN_TIMEOUT` | no | `10s` | positive Go duration | all | no | Graceful SIGTERM shutdown |
| `HTTP_MAX_JSON_BODY_SIZE` | no | `1048576` | positive bytes | all | no | JSON request body limit |
| `VELMIR_USER_PASSWORD` | `cmd/users create`: yes | empty | 12–1024 | CLI only | **yes** | Password through env, never a CLI argument |
| `VITE_BACKEND_BASE_URL` | no | empty | same-origin empty or API base URL | Vite dev | no | Frontend API base |
| `VITE_BACKEND_PROXY_TARGET` | no | `http://127.0.0.1:8080` | backend URL | Vite dev | no | Vite development proxy target |

`COMPOSE_FILE` and `COMPOSE_PROJECT_NAME` belong to Docker Compose, not backend config. Use them to select the same overrides and an isolated project for backup/restore.

## Routine management

Always use the same set of `-f` files. In the explanatory text, `...` means the selected `docker compose -f ...` command, not a literal argument.

```bash
# Start and restart after a stop
docker compose -f docker-compose.yml -f docker-compose.http.yml up -d

# Rebuild and recreate changed services
docker compose -f docker-compose.yml -f docker-compose.http.yml up --build -d

# Status, logs, restart
docker compose -f docker-compose.yml -f docker-compose.http.yml ps
docker compose -f docker-compose.yml -f docker-compose.http.yml logs --tail=100
docker compose -f docker-compose.yml -f docker-compose.http.yml logs -f app
docker compose -f docker-compose.yml -f docker-compose.http.yml restart app

# Stop containers while preserving the project and volumes
docker compose -f docker-compose.yml -f docker-compose.http.yml stop

# Remove containers/network, but NOT named volumes
docker compose -f docker-compose.yml -f docker-compose.http.yml down
```

Never use `down -v` for routine stop, backup, restore, or upgrade: it deletes volumes. To update sources, obtain the new version through your normal process, then create a backup, run `up --build -d`, and perform checks. For a registry image, use `pull` instead of a local rebuild.

The app runs non-root with a read-only rootfs, tmpfs `/tmp`, dropped capabilities, no-new-privileges, a healthcheck, `restart: unless-stopped`, and 10 MiB × 3 file log rotation. Initial resource limits are defined in Compose; tune them for observed load.

## Backup/restore

Backup includes **only the admin database**: users, sessions, roles, resource permissions, field permissions, audit events, and `goose_db_version`. The target DB is not included; its owner is responsible for target backups.

The scripts use the running `admin-db`, `pg_dump` custom format, and the `admin-db-backups` named volume. Never redirect `.dump` through `>` — PowerShell redirection in particular can corrupt binary data.

Shell on Linux/macOS:

```bash
export COMPOSE_FILE=docker-compose.yml:docker-compose.http.yml
sh scripts/backup-admin.sh velmir-admin-before-upgrade.dump
docker compose exec -T admin-db ls -l /backups
```

Windows PowerShell:

```powershell
$env:COMPOSE_FILE = "docker-compose.yml;docker-compose.http.yml"
.\scripts\Backup-Admin.ps1 -BackupName velmir-admin-before-upgrade.dump
docker compose exec -T admin-db ls -l /backups
```

Windows Git Bash also uses `;` because Docker Compose is a Windows binary:

```bash
export COMPOSE_FILE='docker-compose.yml;docker-compose.http.yml'
sh scripts/backup-admin.sh velmir-admin-before-upgrade.dump
```

For demo, replace the HTTP override with `docker-compose.demo.yml`. If needed, set the same `COMPOSE_PROJECT_NAME` as the running stack.

Restore is allowed only into a **fresh database**:

```bash
sh scripts/restore-admin-fresh.sh velmir-admin-before-upgrade.dump velmir_admin_restored
```

```powershell
.\scripts\Restore-AdminFresh.ps1 -BackupName velmir-admin-before-upgrade.dump -NewDatabaseName velmir_admin_restored
```

The scripts create a new DB through `createdb`; an existing name is rejected. They then run `pg_restore --single-transaction --exit-on-error --no-owner --no-acl`. There is no automatic destructive restore. Never restore over production or use `down -v`.

For full isolation, use a separate Compose project, containers, network, volumes, and ports. `TARGET_DATABASE_URL` is **not required** for admin-only restore or verification of users/RBAC/history. Point a separate app at the restored `ADMIN_DATABASE_URL`, run idempotent migrate, and verify the existing login, `/api/auth/me`, roles/permissions, field permissions, and audit. Before upgrading, create a backup and verify that it is non-empty and actually restores into a fresh isolated DB.

## Upgrade/rollback

1. Stop unplanned changes, then create and verify an admin backup.
2. Obtain the new sources/image. Keep the same Compose files and named volumes.
3. Start the admin DB and the separate migrate job, then start the whole stack:

```bash
docker compose -f docker-compose.yml -f docker-compose.http.yml up -d admin-db
docker compose -f docker-compose.yml -f docker-compose.http.yml run --rm migrate
docker compose -f docker-compose.yml -f docker-compose.http.yml run --rm migrate
docker compose -f docker-compose.yml -f docker-compose.http.yml up --build -d
docker compose -f docker-compose.yml -f docker-compose.http.yml ps
```

The second migrate must be a no-op. For TLS/secrets, use the complete original `-f` set; never add the demo override to production. Startup order automatically enforces admin DB health → migrations → bootstrap → app → Caddy.

Rolling back the image does not roll back Goose migrations. Start an old image only if it is compatible with the newer schema. There is no automatic destructive migration rollback. Restoring a backup is a separate manual disaster recovery operation into a fresh DB; changes made after the backup are lost.

## Troubleshooting

- **Port already in use**: inspect `docker compose ps` and listening OS ports. Change `VELMIR_HTTP_PORT`, `VELMIR_TARGET_PORT`, or both `VELMIR_PROXY_*_PORT`; do not start a second stack with the same published ports.
- **`admin-db` unhealthy**: inspect `logs admin-db`, `POSTGRES_DB/USER/PASSWORD`, volume state, and the password match in `ADMIN_DATABASE_URL`. Do not delete the volume to fix credentials.
- **`target-db` unhealthy**: inspect `logs target-db`, the free demo port, and initialization on a fresh volume. For an existing target, inspect its own logs and privileges.
- **Bootstrap failure**: inspect `logs migrate bootstrap`, successful migrate completion, whether `users` is empty, and bootstrap variables. Fix credentials and repeat `up -d`.
- **Missing production credentials**: an empty DB requires `VELMIR_BOOTSTRAP_ADMIN_LOGIN` and `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` or their `_FILE` variants.
- **Trivial credentials rejected**: choose a non-trivial login and a long random 12–1024 character password; `admin/admin` is allowed only in demo.
- **`TARGET_NOT_CONFIGURED` / `not_configured`**: set the single `TARGET_DATABASE_URL` or its `_FILE`; admin-only UI can work without a target.
- **Target unavailable**: check the DNS/service name, port, `sslmode`, network, and `logs app`; `/readyz` returns 503 while the configured target is unavailable.
- **Target permission denied**: the target owner must grant the required SELECT/INSERT/UPDATE/DELETE/sequence privileges. Velmir does not fix this through automatic GRANT.
- **Secure cookies over HTTP**: do not disable Secure in production. Use an HTTPS terminator/Caddy and the same origin; direct HTTP is unsuitable for production login.
- **401 vs 403**: 401 means a missing/expired/revoked session or invalid login; 403 `FORBIDDEN` means insufficient RBAC permission; 403 `CSRF_TOKEN_INVALID` is a separate CSRF problem.
- **CSRF errors**: refresh `/api/auth/me`, use the current `X-CSRF-Token` from login/me, and make same-origin requests. The old token/session is invalid after logout.
- **Trusted proxy/client IP**: check the immediate proxy CIDR in `TRUSTED_PROXY_CIDRS` and forwarding-header replacement by the proxy. Do not trust every CIDR; otherwise client IP/rate limiting can be spoofed.
- **CSP/browser errors**: inspect the browser console and `logs app`. Scripts/API/fonts must be same-origin; inline scripts and `unsafe-eval` are forbidden. Do not weaken CSP for a third-party script.
- **Where to find logs**: `docker compose ... logs --tail=200 app admin-db migrate bootstrap`, plus `caddy` for TLS and `target-db` for demo. `/healthz` is liveness; `/readyz` is component readiness.
- **Support requests**: redact DSNs, passwords, secret file content, session/CSRF tokens, cookies, private domains, and personal data. Attach sanitized `ps`, error codes, and small log excerpts, not full `config` output with env values.

## Production security checklist

- [ ] The administrator has a long random password; `admin/admin` is not used.
- [ ] Bootstrap login/password/email were removed after the first startup; the bootstrap secrets override is no longer included.
- [ ] `APP_ENV=production` and demo mode is disabled.
- [ ] HTTPS is used with `SESSION_COOKIE_SECURE=true`; direct production HTTP is not exposed.
- [ ] DSNs/passwords use secrets files or protected env; secrets are absent from Git/image/frontend bundle.
- [ ] Admin/target DBs are not published on the host; the demo target is loopback-only.
- [ ] `TRUSTED_PROXY_CIDRS` contains only actual proxy networks.
- [ ] The target role has only the required privileges; no automatic DDL/GRANT is needed.
- [ ] Velmir RBAC and field permissions are configured; access is denied by default.
- [ ] A backup was created and successfully restored into a fresh isolated DB.
- [ ] Resource limits and log rotation were checked and adapted to load.
- [ ] Logs, Compose output, and support attachments were checked for secrets.
- [ ] Named volumes are preserved; `down -v` is not used.

# Запуск Velmir

[English version](setup-guide.en.md)

## Назначение и режимы запуска

Velmir — metadata-driven панель администрирования PostgreSQL. Она использует отдельную admin database для пользователей, сессий, ролей, прав и истории изменений и не выполняет миграции, DDL или automatic GRANT в target database.

Поддерживаются следующие Docker-режимы:

- **Local demo**: admin DB и изолированная demo target DB, localhost HTTP, автоматический demo administrator.
- **Production HTTP за внешним HTTPS terminator**: приложение слушает только loopback, а внешний proxy обеспечивает HTTPS.
- **Production Caddy HTTPS**: Caddy публикует HTTP/HTTPS, получает сертификаты и проксирует запросы во внутреннее приложение.
- **Docker secrets**: DSN, пароль PostgreSQL и bootstrap credentials передаются файлами вместо environment values; secrets можно сочетать с HTTP или Caddy HTTPS.

API и детали прав описаны в [документации API](api.md). Краткий обзор проекта находится в [README](../README.md).

## Системные требования

- Windows или macOS: Docker Desktop с Docker Compose v2.
- Linux: Docker Engine с plugin Docker Compose v2.
- Свободные порты: demo `8080` и `5433`; production HTTP — выбранный `VELMIR_HTTP_PORT`; Caddy — `80` и `443` либо выбранные proxy ports.
- Доступ к образам Docker Hub при первой сборке.
- Для Caddy с публичным ACME: зарегистрированный домен, DNS и входящие TCP 80/443.

Для Docker-запуска Go, Node.js, npm и Goose на хосте **не требуются**: frontend, backend и migrate CLI собираются в Docker image. Эти инструменты нужны только для локальной разработки и запуска проверок из исходников.

## Быстрый local demo запуск

В корне репозитория выполните одну команду в shell или PowerShell:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml up --build -d
```

Откройте `http://127.0.0.1:8080` и войдите с login `admin` и password `admin`. Demo override создаёт admin DB и target DB `velmir_demo` с демонстрационными таблицами и связями. App публикуется на `127.0.0.1:8080`, demo target — на `127.0.0.1:5433`; admin DB наружу не публикуется.

**`admin/admin` — небезопасные credentials только для локального demo. Не публикуйте demo в сети и не используйте эти credentials в production.**

Проверка состояния и logs:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml ps
docker compose -f docker-compose.yml -f docker-compose.demo.yml logs --tail=100
docker compose -f docker-compose.yml -f docker-compose.demo.yml logs -f app
```

Остановка без удаления named volumes:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml stop
```

Повторный запуск:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml up -d
```

## Production подготовка

Создайте корневой `.env` из безопасного example:

```bash
cp .env.docker.example .env
```

```powershell
Copy-Item .env.docker.example .env
```

Заполните placeholders собственными значениями. Не копируйте приведённые ниже placeholders как рабочие пароли:

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

- `POSTGRES_PASSWORD` и пароль внутри `ADMIN_DATABASE_URL` должны совпадать; специальные символы в DSN должны быть URL-encoded.
- Внутри Compose admin host называется `admin-db`, а не `localhost`.
- `TARGET_DATABASE_URL` optional: без target работают authentication и administration UI.
- Production требует `APP_ENV=production`, `SESSION_COOKIE_SECURE=true` и built frontend; production Docker image уже содержит frontend.
- `.env`, реальные DSN, пароли, tokens, backups и каталог `secrets/` не должны попадать в Git. Они исключены через `.gitignore`; не удаляйте эти правила.

При использовании secrets оставьте соответствующие secret values в `.env` пустыми и следуйте разделу [Docker secrets](#docker-secrets).

## Первый administrator

Dependency order: `admin-db` healthy → `migrate` completed successfully → `bootstrap` completed successfully → `app`. Текущая admin schema version — **Goose 9**.

Bootstrap создаёт administrator только если таблица `users` пуста и автоматически назначает системную роль `admin`:

- login обязателен, не содержит whitespace и имеет длину не более 254 символов;
- password обязателен, содержит 12–1024 символа и должен быть длинным случайным значением;
- display name optional, по умолчанию `Administrator`, максимум 200 символов;
- production отклоняет missing/weak credentials и список trivial passwords; `admin/admin` запрещён;
- `admin/admin` создаётся только при явном demo override с `APP_ENV=development` и `VELMIR_DEMO_MODE=true`.

После первого успешного запуска удалите `VELMIR_BOOTSTRAP_ADMIN_LOGIN`, legacy `VELMIR_BOOTSTRAP_ADMIN_EMAIL` и `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` из `.env`/окружения. В secrets mode больше не подключайте `docker-compose.bootstrap-secrets.yml`.

Bootstrap выполняется одноразово: если хотя бы один пользователь уже существует, он ничего не создаёт и **не сбрасывает пароль**. Поэтому повторный запуск без bootstrap credentials безопасен и сохраняет login/password существующего administrator.

## Target database

`TARGET_DATABASE_URL` — единый DSN для read pool и mutation pool. Отдельный writer DSN не поддерживается. Read pool всегда устанавливает `default_transaction_read_only=on`; mutation pool используется только для поддерживаемых INSERT/UPDATE/DELETE после session, CSRF, resource RBAC и field-level checks.

Владелец target database заранее выдаёт application role минимально необходимые privileges:

- `CONNECT` на database;
- `USAGE` на используемые schemas;
- `SELECT` для чтения;
- `INSERT`, `UPDATE`, `DELETE` только на разрешённые таблицы для mutations;
- `USAGE`, `SELECT`, `UPDATE` на sequences, необходимые для default/identity values.

Для read-only достаточно `CONNECT`, schema `USAGE` и `SELECT`. Velmir не выполняет automatic DDL, migrations или GRANT в target. Права `CREATE`, `ALTER`, `DROP`, `TRUNCATE` приложению не нужны.

Target optional. Без DSN `/readyz` сообщает `target_database=not_configured`, а `/api/schema` — `TARGET_NOT_CONFIGURED`. Недоступный configured target сообщает `unavailable` и делает `/readyz` неуспешным; `/healthz` остаётся независимым liveness check.

PostgreSQL privileges не заменяют Velmir RBAC: authenticated user дополнительно должен иметь resource/action permission, а field permissions ограничивают read/create/update на уровне колонок.

Внутри Compose используйте DNS service names, например `admin-db` или ваш `target-db`, а не `localhost`: `localhost` внутри container указывает на сам container. Для внешней target DB укажите доступный из container hostname/IP и подходящий `sslmode`.

## Production HTTP за внешним proxy

Настройте `.env`, затем запустите:

```bash
docker compose -f docker-compose.yml -f docker-compose.http.yml up --build -d
```

App публикуется только как `127.0.0.1:${VELMIR_HTTP_PORT:-8080}`. Admin DB host port отсутствует. Этот режим предназначен для внешнего same-origin HTTPS terminator на том же узле; не публикуйте app напрямую как production HTTP.

- Сохраните `SESSION_COOKIE_SECURE=true`. Браузер не отправит Secure session cookie по прямому HTTP, поэтому production login должен проходить через HTTPS proxy.
- Proxy должен сохранять исходный Host, передавать корректный `X-Forwarded-Proto` и заменять client forwarding headers, а не доверять значениям клиента.
- Задайте `TRUSTED_PROXY_CIDRS` как comma-separated список только фактических Docker gateway/proxy CIDRs. Не используйте `0.0.0.0/0` или `::/0`.
- Проверки: `docker compose -f docker-compose.yml -f docker-compose.http.yml ps` и `... logs app`.

## Production Caddy HTTPS

1. Настройте DNS A и/или AAAA записи домена на сервер и откройте входящие TCP 80/443.
2. Задайте собственные значения:

```dotenv
VELMIR_DOMAIN=admin.example.test
VELMIR_ACME_EMAIL=admin@example.test
VELMIR_DOCKER_SUBNET=172.30.0.0/24
TRUSTED_PROXY_CIDRS=172.30.0.0/24
```

`admin.example.test` — только placeholder. Если subnet занят, измените **обе** CIDR variables на один свободный subnet. При нестандартных ports используйте `VELMIR_PROXY_HTTP_PORT` и `VELMIR_PROXY_HTTPS_PORT`.

3. Запустите:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml up --build -d
```

Caddy публикует только proxy ports 80/443, перенаправляет HTTP → HTTPS и проксирует запросы к `app:8080`. App и admin DB не имеют host ports. Certificates и ACME account сохраняются в named volumes `caddy-data` и `caddy-config`.

Проверка конфигурации и reload:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml exec caddy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker compose -f docker-compose.yml -f docker-compose.tls.yml exec caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
```

Локальный `caddy validate` и internal HTTPS smoke не доказывают получение публичного ACME certificate. Публичный ACME проверяется только с реальным DNS-доменом и доступными из интернета ports; проверяйте `logs caddy` и certificate в браузере.

## Docker secrets

Поддерживаемые backend `_FILE` variables:

- `ADMIN_DATABASE_URL_FILE`;
- `TARGET_DATABASE_URL_FILE`;
- `VELMIR_BOOTSTRAP_ADMIN_LOGIN_FILE`;
- `VELMIR_BOOTSTRAP_ADMIN_PASSWORD_FILE`;
- legacy `VELMIR_BOOTSTRAP_ADMIN_EMAIL_FILE` как fallback login, если используется собственный secrets mount.

`POSTGRES_PASSWORD_FILE` поддерживается официальным PostgreSQL image. Compose secrets overrides монтируют первые четыре основные переменные; legacy email file требует собственной конфигурации. Для display name `_FILE` нет.

Nonempty env и соответствующий `_FILE` взаимоисключаемы. Файл должен существовать, быть readable и содержать непустое значение; trailing CR/LF удаляются. В DSN используется содержимое файла, а не путь. Пустой/unreadable file вызывает config error без вывода содержимого.

Создайте secret files редактором, не помещая secrets в историю команд:

```bash
mkdir -p secrets
chmod 700 secrets
# Создайте каждый файл редактором, затем:
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

Ограничьте доступ к каталогу средствами ОС. `secrets/`, `*.dump` и `backups/` исключены из Git и Docker build context. Не выводите файлы в terminal и не прикладывайте их к support requests.

Первый production secrets + Caddy HTTPS запуск в shell и PowerShell:

```bash
docker compose -f docker-compose.yml -f docker-compose.secrets.yml -f docker-compose.bootstrap-secrets.yml -f docker-compose.target-secret.yml -f docker-compose.tls.yml up --build -d
```

Без target уберите `docker-compose.target-secret.yml`. Для production HTTP замените TLS override:

```bash
docker compose -f docker-compose.yml -f docker-compose.secrets.yml -f docker-compose.bootstrap-secrets.yml -f docker-compose.http.yml up --build -d
```

После создания administrator повторяйте запуск без `docker-compose.bootstrap-secrets.yml`. Secret file paths не являются секретами; их содержимое — является. Secrets не копируются в image или frontend bundle.

## Полная таблица environment variables

**Prod** — base/HTTP/TLS production Compose; **demo** — demo override; **dev** — запуск backend/Vite вне Docker. Defaults ниже различают backend и Compose там, где это важно. Для `_FILE` секретом является содержимое файла.

| Variable | Required | Default | Allowed values | Mode | Secret | Назначение |
|---|---|---|---|---|---|---|
| `POSTGRES_DB` | prod: да; demo: задано | нет | PostgreSQL DB name | prod/demo | нет | Имя admin DB |
| `POSTGRES_USER` | prod: да; demo: задано | нет | PostgreSQL role | prod/demo | нет | Роль admin DB |
| `POSTGRES_PASSWORD` | env prod: да | нет | nonempty password | prod/demo | **да** | Пароль admin DB |
| `POSTGRES_PASSWORD_FILE` | secrets: вместо env | пусто | readable file path | prod | содержимое **да** | Native PostgreSQL password file |
| `ADMIN_DATABASE_URL` | prod/dev: да; demo: задано | нет | PostgreSQL DSN | все | **да** | Admin DSN для migrate/bootstrap/app |
| `ADMIN_DATABASE_URL_FILE` | вместо env в secrets | пусто | readable file path | prod/dev | содержимое **да** | Файл с admin DSN |
| `TARGET_DATABASE_URL` | нет | пусто | PostgreSQL DSN или пусто | все | **да** | Единый DSN read/mutation pools |
| `TARGET_DATABASE_URL_FILE` | нет; вместо env | пусто | readable file path | prod/dev | содержимое **да** | Файл с target DSN |
| `TARGET_WRITE_DATABASE_URL` | не задавать | пусто | только legacy rejection | migration | **да** | Удалённый writer DSN; удалите и используйте `TARGET_DATABASE_URL` |
| `VELMIR_DEPRECATED_TARGET_WRITE_DATABASE_URL` | не задавать | пусто | Compose legacy mapping | compatibility | **да** | Вызывает ту же explicit legacy rejection |
| `APP_ENV` | нет | backend `development`; Compose `production` | `development`, `production` | все | нет | Security mode |
| `SESSION_COOKIE_SECURE` | prod: `true` | backend `false`; Compose `true` | `true`, `false` | все | нет | Secure flag authentication cookies |
| `VELMIR_BOOTSTRAP_ADMIN_LOGIN` | пустая prod DB: да | пусто; demo fallback | ≤254, без whitespace | все | нет | Login первого administrator |
| `VELMIR_BOOTSTRAP_ADMIN_LOGIN_FILE` | вместо login env | пусто | readable file path | prod/dev | нет | Файл login первого administrator |
| `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` | пустая prod DB: да | пусто; demo fallback | 12–1024, non-trivial | все | **да** | Password первого administrator |
| `VELMIR_BOOTSTRAP_ADMIN_PASSWORD_FILE` | вместо password env | пусто | readable file path | prod/dev | содержимое **да** | Файл password первого administrator |
| `VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME` | нет | `Administrator` | ≤200 | все | нет | Display name первого administrator |
| `VELMIR_BOOTSTRAP_ADMIN_EMAIL` | нет | пусто | legacy login fallback | все | нет | Legacy login; предпочитайте `..._LOGIN` |
| `VELMIR_BOOTSTRAP_ADMIN_EMAIL_FILE` | нет | пусто | readable file path | prod/dev | нет | Legacy login из собственного file mount |
| `VELMIR_DEMO_MODE` | demo: `true`; prod: `false` | `false` | `true`, `false` | demo/prod | нет | Разрешает demo-only bootstrap |
| `VELMIR_HTTP_PORT` | нет | `8080` | TCP port | HTTP/demo | нет | Loopback app host port |
| `VELMIR_PROXY_HTTP_PORT` | нет | `80` | TCP port | TLS | нет | Caddy host/container HTTP port |
| `VELMIR_PROXY_HTTPS_PORT` | нет | `443` | TCP port | TLS | нет | Caddy host/container HTTPS port |
| `VELMIR_DOMAIN` | TLS: да | нет | DNS hostname | TLS | нет | Public Caddy site name |
| `VELMIR_ACME_EMAIL` | TLS: да | нет | email | TLS | нет | ACME account email |
| `VELMIR_DOCKER_SUBNET` | нет | `172.30.0.0/24` | free CIDR | TLS | нет | TLS Compose network subnet |
| `TRUSTED_PROXY_CIDRS` | TLS: да; иначе optional | пусто | comma-separated CIDRs | prod/dev/TLS | нет | Trusted immediate proxy networks |
| `VELMIR_TARGET_PORT` | нет | `5433` | TCP port | demo | нет | Demo target loopback host port |
| `VELMIR_IMAGE` | нет | `velmir:local` | Docker image reference | prod/demo/TLS | нет | Application image |
| `HTTP_ADDR` | нет | backend `127.0.0.1:8080`; Compose `0.0.0.0:8080` | host:port | dev; Compose fixed | нет | Backend listen address |
| `FRONTEND_DIST_PATH` | production вне image: да | backend пусто; image `/app/frontend/dist` | existing dist directory | все | нет | Built Vue directory |
| `LOG_LEVEL` | нет | `info` | `debug`, `info`, `warn`, `error` | все | нет | Structured log level |
| `SESSION_IDLE_TIMEOUT` | нет | `30m` | positive Go duration | все | нет | Idle session lifetime |
| `SESSION_ABSOLUTE_TIMEOUT` | нет | `24h` | Go duration ≥ idle | все | нет | Absolute session lifetime |
| `SESSION_CLEANUP_INTERVAL` | нет | `15m` | positive Go duration | все | нет | Expired-session cleanup period |
| `SESSION_CLEANUP_BATCH_SIZE` | нет | `1000` | positive integer | все | нет | Maximum rows per cleanup batch |
| `DATABASE_CONNECT_TIMEOUT` | нет | `5s` | positive Go duration | все | нет | DB connect timeout |
| `ADMIN_DATABASE_MAX_CONNS` | нет | `10` | positive integer | все | нет | Admin pool max connections |
| `TARGET_DATABASE_MAX_CONNS` | нет | `10` | positive integer | все | нет | Max connections в каждом target pool |
| `DATABASE_MAX_CONN_LIFETIME` | нет | `30m` | positive Go duration | все | нет | Connection lifetime |
| `DATABASE_MAX_CONN_IDLE_TIME` | нет | `5m` | positive Go duration | все | нет | Idle connection time |
| `HTTP_READ_HEADER_TIMEOUT` | нет | `5s` | positive Go duration | все | нет | Read headers timeout |
| `HTTP_READ_TIMEOUT` | нет | `15s` | positive Go duration | все | нет | Request read timeout |
| `HTTP_WRITE_TIMEOUT` | нет | `30s` | positive Go duration | все | нет | Response write timeout |
| `HTTP_IDLE_TIMEOUT` | нет | `60s` | positive Go duration | все | нет | Keep-alive idle timeout |
| `HTTP_SHUTDOWN_TIMEOUT` | нет | `10s` | positive Go duration | все | нет | Graceful SIGTERM shutdown |
| `HTTP_MAX_JSON_BODY_SIZE` | нет | `1048576` | positive bytes | все | нет | JSON request body limit |
| `VELMIR_USER_PASSWORD` | `cmd/users create`: да | пусто | 12–1024 | CLI only | **да** | Password через env, никогда CLI argument |
| `VITE_BACKEND_BASE_URL` | нет | пусто | same-origin empty или API base URL | Vite dev | нет | Frontend API base |
| `VITE_BACKEND_PROXY_TARGET` | нет | `http://127.0.0.1:8080` | backend URL | Vite dev | нет | Vite development proxy target |

`COMPOSE_FILE` и `COMPOSE_PROJECT_NAME` относятся к Docker Compose, а не к backend config. Используйте их для выбора того же набора overrides и изолированного project при backup/restore.

## Повседневное управление

Всегда используйте один и тот же набор `-f`. Ниже `...` означает выбранную команду `docker compose -f ...`, а не literal argument.

```bash
# Запуск и повторный запуск
docker compose -f docker-compose.yml -f docker-compose.http.yml up -d

# Rebuild и пересоздание изменённых services
docker compose -f docker-compose.yml -f docker-compose.http.yml up --build -d

# Состояние, logs, restart
docker compose -f docker-compose.yml -f docker-compose.http.yml ps
docker compose -f docker-compose.yml -f docker-compose.http.yml logs --tail=100
docker compose -f docker-compose.yml -f docker-compose.http.yml logs -f app
docker compose -f docker-compose.yml -f docker-compose.http.yml restart app

# Остановка containers с сохранением project и volumes
docker compose -f docker-compose.yml -f docker-compose.http.yml stop

# Удаление containers/network, но НЕ named volumes
docker compose -f docker-compose.yml -f docker-compose.http.yml down
```

Никогда не используйте `down -v` при routine stop, backup, restore или upgrade: это удаляет volumes. Для обновления исходников получите новую версию своим обычным способом, затем выполните backup, `up --build -d` и проверки. Для registry image используйте `pull` вместо локального rebuild.

App работает non-root с read-only rootfs, tmpfs `/tmp`, dropped capabilities, no-new-privileges, healthcheck, `restart: unless-stopped` и log rotation 10 MiB × 3 files. Начальные resource limits заданы в Compose; настройте их по фактической нагрузке.

## Backup/restore

Backup включает **только admin database**: users, sessions, roles, resource permissions, field permissions, audit events и `goose_db_version`. Target DB в backup не входит; её резервное копирование выполняет владелец target.

Scripts используют running `admin-db`, `pg_dump` custom format и named volume `admin-db-backups`. Не перенаправляйте `.dump` через `>` — особенно в PowerShell это может повредить binary.

Shell на Linux/macOS:

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

Windows Git Bash также использует `;`, поскольку Docker Compose является Windows binary:

```bash
export COMPOSE_FILE='docker-compose.yml;docker-compose.http.yml'
sh scripts/backup-admin.sh velmir-admin-before-upgrade.dump
```

Для demo замените HTTP override на `docker-compose.demo.yml`. При необходимости задайте тот же `COMPOSE_PROJECT_NAME`, что у running stack.

Restore разрешён только в **fresh database**:

```bash
sh scripts/restore-admin-fresh.sh velmir-admin-before-upgrade.dump velmir_admin_restored
```

```powershell
.\scripts\Restore-AdminFresh.ps1 -BackupName velmir-admin-before-upgrade.dump -NewDatabaseName velmir_admin_restored
```

Scripts создают новую DB через `createdb`; существующее имя отклоняется. Затем выполняется `pg_restore --single-transaction --exit-on-error --no-owner --no-acl`. Automatic destructive restore отсутствует. Не восстанавливайте поверх production и не используйте `down -v`.

Для полной изоляции используйте отдельный Compose project, containers, network, volumes и ports. `TARGET_DATABASE_URL` для admin-only restore и проверки users/RBAC/history **не нужен**. Направьте отдельный app на restored `ADMIN_DATABASE_URL`, запустите idempotent migrate, проверьте существующий login, `/api/auth/me`, roles/permissions, field permissions и audit. Перед upgrade создайте backup и убедитесь, что он nonempty и действительно восстанавливается в fresh isolated DB.

## Upgrade/rollback

1. Остановите незапланированные изменения, создайте и проверьте admin backup.
2. Получите новые sources/image. Сохраните тот же набор Compose files и named volumes.
3. Запустите admin DB и отдельный migrate job, затем весь stack:

```bash
docker compose -f docker-compose.yml -f docker-compose.http.yml up -d admin-db
docker compose -f docker-compose.yml -f docker-compose.http.yml run --rm migrate
docker compose -f docker-compose.yml -f docker-compose.http.yml run --rm migrate
docker compose -f docker-compose.yml -f docker-compose.http.yml up --build -d
docker compose -f docker-compose.yml -f docker-compose.http.yml ps
```

Второй migrate должен быть no-op. Для TLS/secrets используйте полный исходный набор `-f`; не добавляйте demo override в production. Startup order автоматически соблюдает admin DB health → migrations → bootstrap → app → Caddy.

Rollback image не откатывает Goose migrations. Запускайте прежний image только если он совместим с новой schema. Automatic destructive migration rollback нет. Restore backup — отдельная manual disaster recovery операция в fresh DB; изменения после backup будут потеряны.

## Troubleshooting

- **Port уже занят**: проверьте `docker compose ps` и слушающие ports ОС. Измените `VELMIR_HTTP_PORT`, `VELMIR_TARGET_PORT` или оба `VELMIR_PROXY_*_PORT`; не запускайте второй stack с теми же published ports.
- **`admin-db` unhealthy**: проверьте `logs admin-db`, `POSTGRES_DB/USER/PASSWORD`, состояние volume и совпадение пароля в `ADMIN_DATABASE_URL`. Не удаляйте volume для исправления credentials.
- **`target-db` unhealthy**: проверьте `logs target-db`, свободный demo port и инициализацию fresh volume. Для существующей target проверьте её собственные logs и privileges.
- **Bootstrap failure**: проверьте `logs migrate bootstrap`, успешное завершение migrate, пустота `users` и bootstrap variables. Исправьте credentials и повторите `up -d`.
- **Missing production credentials**: пустая DB требует `VELMIR_BOOTSTRAP_ADMIN_LOGIN` и `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` либо соответствующие `_FILE`.
- **Trivial credentials rejected**: выберите нетривиальный login и длинный случайный password 12–1024; `admin/admin` разрешён только в demo.
- **`TARGET_NOT_CONFIGURED` / `not_configured`**: задайте единый `TARGET_DATABASE_URL` или его `_FILE`; admin-only UI может работать без target.
- **Target unavailable**: проверьте DNS/service name, port, `sslmode`, network и `logs app`; `/readyz` будет 503, пока configured target недоступен.
- **Target permission denied**: владелец target должен выдать необходимые SELECT/INSERT/UPDATE/DELETE/sequence privileges. Velmir не исправляет это automatic GRANT.
- **Secure cookies через HTTP**: не отключайте Secure в production. Используйте HTTPS terminator/Caddy и тот же origin; direct HTTP не подходит для production login.
- **401 vs 403**: 401 означает missing/expired/revoked session или invalid login; 403 `FORBIDDEN` — недостаточно RBAC permission; 403 `CSRF_TOKEN_INVALID` — отдельная CSRF проблема.
- **CSRF errors**: обновите `/api/auth/me`, используйте текущий `X-CSRF-Token` из login/me и same-origin requests. После logout старый token/session недействительны.
- **Trusted proxy/client IP**: проверьте immediate proxy CIDR в `TRUSTED_PROXY_CIDRS` и замену forwarding headers proxy. Не доверяйте всем CIDRs; иначе client IP/rate limiting могут быть подделаны.
- **CSP/browser errors**: проверяйте browser console и `logs app`. Scripts/API/fonts должны быть same-origin; inline scripts и `unsafe-eval` запрещены. Не ослабляйте CSP ради стороннего script.
- **Где logs**: `docker compose ... logs --tail=200 app admin-db migrate bootstrap`, для TLS также `caddy`, для demo также `target-db`. `/healthz` — liveness, `/readyz` — component readiness.
- **Support requests**: удаляйте DSN, passwords, secret file contents, session/CSRF tokens, cookies, private domains и personal data. Прикладывайте sanitized `ps`, error codes и небольшой фрагмент logs, не полный `config` с env values.

## Production security checklist

- [ ] Administrator имеет длинный случайный пароль; `admin/admin` не используется.
- [ ] Bootstrap login/password/email удалены после первого запуска; bootstrap secrets override больше не подключён.
- [ ] `APP_ENV=production`, demo mode выключен.
- [ ] Используется HTTPS, `SESSION_COOKIE_SECURE=true`; direct production HTTP не открыт.
- [ ] DSN/passwords передаются через secrets files или защищённое env; secrets отсутствуют в Git/image/frontend bundle.
- [ ] Admin/target DB не опубликованы на host; demo target доступен только на loopback.
- [ ] `TRUSTED_PROXY_CIDRS` содержит только фактические proxy networks.
- [ ] Target role имеет минимально необходимые privileges; automatic DDL/GRANT не требуются.
- [ ] Velmir RBAC и field permissions настроены; доступ по умолчанию запрещён.
- [ ] Backup создан и успешно восстановлен в fresh isolated DB.
- [ ] Resource limits и log rotation проверены и адаптированы к нагрузке.
- [ ] Logs, Compose output и support attachments проверены на отсутствие secrets.
- [ ] Named volumes сохраняются; `down -v` не используется.

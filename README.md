# Velmir

[English version](README_EN.md)

Velmir — панель администрирования PostgreSQL, управляемая метаданными. На текущем этапе реализованы introspection схемы, универсальные списки записей с пагинацией, сортировкой, поиском и типизированными фильтрами, related labels, автоматические формы создания и редактирования с many-to-one relation selectors, read-only one-to-many panels и базовый many-to-many editor через junction tables, удаление записей с подтверждением, история изменений через Velmir, аутентификация с серверными сессиями, RBAC и управление пользователями и ролями через UI. Nested mutations, файлы и автоматизация пока не реализованы.

## Требования

- Go **1.26.0** (см. `backend/go.mod`)
- Node.js и npm
- Docker с Docker Compose

## Docker Compose

Единый image собирается в три стадии: Node.js выполняет `npm ci`, type-check и build Vue; Go собирает server, migrate, bootstrap и users CLI; минимальный Alpine runtime содержит только non-root process, CA certificates, admin migrations и собранный frontend. Исходники frontend, `.env`, Node и Go toolchain в runtime не копируются.

```text
admin-db ──healthy──> migrate ──completed──> bootstrap ──completed──> app
                                                                    ├─ /api/*
                                                                    └─ Vue + SPA fallback
Demo override дополнительно создаёт isolated target-db; app использует один TARGET_DATABASE_URL.
```

### Demo одной командой

Настройка `.env` для demo не требуется:

```bash
docker compose -f docker-compose.yml -f docker-compose.demo.yml up --build -d
```

Откройте `http://127.0.0.1:8080`. При пустой admin database создаются **только для localhost demo** логин `admin` и пароль `admin`; предупреждение выводится в лог без повторного раскрытия пароля. Не публикуйте этот режим в сети и не переносите demo credentials в production. Demo включается только явным вторым `-f`, а target публикуется на `127.0.0.1:5433` для локальных integration-тестов. Admin DB наружу не публикуется.

### Production без Caddy TLS proxy

1. Скопируйте `.env.docker.example` в корневой `.env`, замените placeholders, задайте длинный случайный `POSTGRES_PASSWORD`, совпадающий `ADMIN_DATABASE_URL` и безопасные bootstrap credentials. Пароли внутри DSN должны быть URL-encoded.
2. При необходимости задайте единый `TARGET_DATABASE_URL`. Без него auth/admin UI работает, но target resources не настроены.
3. Запустите HTTP на loopback:

```bash
docker compose -f docker-compose.yml -f docker-compose.http.yml up --build -d
```

App публикуется только как `127.0.0.1:${VELMIR_HTTP_PORT:-8080}`. Этот режим предназначен для localhost или внешнего HTTPS terminator с тем же origin. Для proxy задайте `TRUSTED_PROXY_CIDRS` только на фактический Docker gateway/proxy CIDR. `APP_ENV=production` требует `SESSION_COOKIE_SECURE=true` и built frontend; прямой HTTP не подходит для production login с Secure cookies.

### Production с Caddy HTTPS

1. Настройте DNS домена на сервер и откройте входящие TCP 80/443. Задайте в `.env` собственные значения, например:

```dotenv
VELMIR_DOMAIN=admin.example.test
VELMIR_ACME_EMAIL=admin@example.test
VELMIR_DOCKER_SUBNET=172.30.0.0/24
TRUSTED_PROXY_CIDRS=172.30.0.0/24
```

`admin.example.test` — только placeholder. Если subnet уже занят, измените **обе** переменные на один свободный CIDR. Не используйте `0.0.0.0/0` или `::/0` для trusted proxies.

2. Запустите production HTTPS одной командой:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml up --build -d
```

Caddy публикует только host ports 80/443, автоматически получает и обновляет ACME certificates и перенаправляет HTTP на HTTPS. App и admin DB не публикуют host ports; app принимает только internal Docker traffic от Caddy. Caddy передаёт Host, протокол и перезаписывает client forwarding headers. Admin API слушает только `127.0.0.1:2019` внутри Caddy container и наружу не публикуется. `caddy-data` и `caddy-config` — отдельные named volumes; `restart` и `caddy reload` не удаляют certificates:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml exec caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
```

Go server по-прежнему отдаёт same-origin static/API/SPA; Caddy является дополнительным proxy и не заменяет static server.

### Bootstrap и управление Compose

После первого успешного production запуска удалите bootstrap login/password/email из `.env` и окружения или не подключайте `docker-compose.bootstrap-secrets.yml` при следующих запусках. Пока существует пользователь, bootstrap ничего не создаёт и не меняет пароль. Production по-прежнему отклоняет missing/weak/trivial credentials для пустой DB; `admin/admin` доступен только в явном development demo.

Для выбранного набора `-f` используйте обычные `ps`, `logs`, `stop`, `restart` и `up -d`. `down` без `-v` сохраняет named volumes. `admin-db-data`, `admin-db-backups`, demo `target-db-data`, `caddy-data` и `caddy-config` не удаляются при restart или обычном `down`. Никогда не используйте `down -v` как шаг backup, restore или upgrade.

App работает non-root с read-only root filesystem, tmpfs `/tmp`, `cap_drop: ALL`, no-new-privileges, healthcheck, restart policy и log rotation. Начальные limits: app 512 MiB/1 CPU/128 PIDs, admin DB 1 GiB/1 CPU/256 PIDs, Caddy 256 MiB/0.5 CPU/64 PIDs. Это исходные значения для проверки, а не рекомендация для любой нагрузки; изменяйте их по наблюдаемым метрикам.

### Security headers и client IP

Единственный владелец headers — Go app; Caddy не задаёт конкурирующие значения. Все ответы получают CSP, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY` + `frame-ancestors 'none'` и restrictive `Permissions-Policy`. Production HTTPS дополнительно получает `Strict-Transport-Security: max-age=31536000` без `includeSubDomains` и `preload`; localhost demo HSTS не выдаёт.

CSP разрешает только `'self'` для scripts/API/fonts и `'self' data:` для images. Scripts не имеют `unsafe-inline`/`unsafe-eval`, wildcard sources нет. `style-src 'self' 'unsafe-inline'` — единственное исключение: Vue использует runtime inline style attributes для динамической раскладки и controls. Logo, favicon, dialogs и relation selectors остаются same-origin.

По умолчанию `TRUSTED_PROXY_CIDRS` пуст и `X-Forwarded-For`/`X-Real-IP` игнорируются. Headers учитываются только когда immediate TCP peer входит в доверенный CIDR; цепочка проверяется справа налево, поэтому forged leftmost entries не меняют client IP для login rate limiter. `X-Forwarded-Host` для redirects не используется. В TLS Compose app не имеет host-port, поэтому внешний клиент не может обойти Caddy и подменить IP.

### Полная таблица окружения

Корневой `.env` используется Docker Compose и локальными Go CLI через `godotenv`; `frontend/.env` нужен только Vite. **Prod** — основной Compose, **TLS** — Caddy override, **demo** — demo override. Для `_FILE` переменных секретом является содержимое файла, не путь.

| Переменная | Назначение | Обязательность | Default | Режим | Секрет | Безопасный пример |
|---|---|---|---|---|---|---|
| `POSTGRES_DB` | Имя admin DB | prod: да; demo: задано | нет | prod/demo | нет | `velmir_admin` |
| `POSTGRES_USER` | Роль admin DB | prod: да; demo: задано | нет | prod/demo | нет | `velmir` |
| `POSTGRES_PASSWORD` | Пароль admin DB | prod env: да | нет | prod/demo | **да** | `<long-random-password>` |
| `POSTGRES_PASSWORD_FILE` | Native PostgreSQL secret file | Вместо password в secrets mode | пусто | prod | путь нет; файл **да** | `/run/secrets/postgres_password` |
| `ADMIN_DATABASE_URL` | DSN admin DB | prod/dev: да; demo: задано | нет | все | **да** | `postgresql://velmir:<password>@admin-db:5432/velmir_admin?sslmode=disable` |
| `ADMIN_DATABASE_URL_FILE` | Файл с admin DSN | Вместо env в secrets mode | пусто | prod/dev | путь нет; файл **да** | `/run/secrets/admin_database_url` |
| `TARGET_DATABASE_URL` | Единый DSN read/mutation pools | нет | пусто | все | **да** | `postgresql://app:<password>@target-db:5432/app?sslmode=require` |
| `TARGET_DATABASE_URL_FILE` | Файл с единым target DSN | нет; вместо env | пусто | prod/dev | путь нет; файл **да** | `/run/secrets/target_database_url` |
| `VELMIR_DEPRECATED_TARGET_WRITE_DATABASE_URL` | Legacy migration error; не pool | не задавать | пусто | compatibility | **да** | пусто |
| `APP_ENV` | Security mode | нет | backend `development`; Compose `production` | все | нет | `production` |
| `SESSION_COOKIE_SECURE` | Secure cookie flag | prod: `true` | backend `false`; Compose `true` | все | нет | `true` |
| `VELMIR_BOOTSTRAP_ADMIN_LOGIN` | Login первого admin | Только пустая prod DB | пусто; demo fallback | все | нет | `admin@example.test` |
| `VELMIR_BOOTSTRAP_ADMIN_LOGIN_FILE` | Файл login первого admin | Альтернатива env | пусто | prod/dev | путь нет; файл нет | `/run/secrets/bootstrap_admin_login` |
| `VELMIR_BOOTSTRAP_ADMIN_PASSWORD` | Пароль первого admin | Только пустая prod DB | пусто; demo fallback | все | **да** | `<long-random-password>` |
| `VELMIR_BOOTSTRAP_ADMIN_PASSWORD_FILE` | Файл пароля первого admin | Альтернатива env | пусто | prod/dev | путь нет; файл **да** | `/run/secrets/bootstrap_admin_password` |
| `VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME` | Имя первого admin | нет | `Administrator` | все | нет | `Administrator` |
| `VELMIR_BOOTSTRAP_ADMIN_EMAIL` / `_FILE` | Legacy login fallback | нет | пусто | все | env нет; file содержимое нет | `admin@example.test` |
| `VELMIR_DEMO_MODE` | Demo-only bootstrap | Только demo | `false` | demo | нет | `true` |
| `VELMIR_HTTP_PORT` | Non-TLS host HTTP port | нет | `8080` | HTTP/demo | нет | `8080` |
| `VELMIR_PROXY_HTTP_PORT` | Caddy host HTTP port | нет | `80` | TLS | нет | `80` |
| `VELMIR_PROXY_HTTPS_PORT` | Caddy host HTTPS port | нет | `443` | TLS | нет | `443` |
| `VELMIR_DOMAIN` | Public Caddy site name | TLS: да | нет | TLS | нет | `admin.example.test` |
| `VELMIR_ACME_EMAIL` | ACME account email | TLS: да | нет | TLS | нет | `admin@example.test` |
| `VELMIR_DOCKER_SUBNET` | TLS Compose network subnet | нет | `172.30.0.0/24` | TLS | нет | `172.30.0.0/24` |
| `TRUSTED_PROXY_CIDRS` | Trusted immediate proxies | TLS: да; иначе нет | пусто | prod/dev/TLS | нет | `172.30.0.0/24` |
| `VELMIR_TARGET_PORT` | Demo target host port | нет | `5433` | demo | нет | `5433` |
| `VELMIR_IMAGE` | Application image | нет | `velmir:local` | prod/demo/TLS | нет | `velmir:local` |
| `HTTP_ADDR` | Backend listen address | нет | `127.0.0.1:8080`; Compose `0.0.0.0:8080` | dev; Compose fixed | нет | `127.0.0.1:8080` |
| `FRONTEND_DIST_PATH` | Built Vue directory | prod: да | image `/app/frontend/dist` | все | нет | `/app/frontend/dist` |
| `LOG_LEVEL` | Structured log level | нет | `info` | все | нет | `info` |
| `SESSION_IDLE_TIMEOUT` | Idle session lifetime | нет | `30m` | все | нет | `30m` |
| `SESSION_ABSOLUTE_TIMEOUT` | Absolute session lifetime | нет | `24h`, не меньше idle | все | нет | `24h` |
| `SESSION_CLEANUP_INTERVAL` | Период cleanup | нет | `15m` | все | нет | `15m` |
| `SESSION_CLEANUP_BATCH_SIZE` | Max rows за cleanup batch | нет | `1000` | все | нет | `1000` |
| `DATABASE_CONNECT_TIMEOUT` | DB connect timeout | нет | `5s` | все | нет | `5s` |
| `ADMIN_DATABASE_MAX_CONNS` | Admin pool max connections | нет | `10` | все | нет | `10` |
| `TARGET_DATABASE_MAX_CONNS` | Max connections в каждом target pool | нет | `10` | все | нет | `10` |
| `DATABASE_MAX_CONN_LIFETIME` | Connection lifetime | нет | `30m` | все | нет | `30m` |
| `DATABASE_MAX_CONN_IDLE_TIME` | Idle connection time | нет | `5m` | все | нет | `5m` |
| `HTTP_READ_HEADER_TIMEOUT` | Read headers timeout | нет | `5s` | все | нет | `5s` |
| `HTTP_READ_TIMEOUT` | Read timeout | нет | `15s` | все | нет | `15s` |
| `HTTP_WRITE_TIMEOUT` | Write timeout | нет | `30s` | все | нет | `30s` |
| `HTTP_IDLE_TIMEOUT` | Keep-alive idle timeout | нет | `60s` | все | нет | `60s` |
| `HTTP_SHUTDOWN_TIMEOUT` | Graceful SIGTERM shutdown | нет | `10s` | все | нет | `10s` |
| `HTTP_MAX_JSON_BODY_SIZE` | JSON body limit in bytes | нет | `1048576` | все | нет | `1048576` |

Для `cmd/users` пароль передаётся только через secret `VELMIR_USER_PASSWORD` (12–1024), не через аргументы. Vite development использует non-secret `VITE_BACKEND_BASE_URL=` и `VITE_BACKEND_PROXY_TARGET=http://127.0.0.1:8080`. Production bundle не получает DSN/passwords и использует same-origin API.

Nonempty env и соответствующий `_FILE` взаимоисключаемы. Пустой/unreadable file вызывает config error без вывода содержимого; в DSN подставляется именно содержимое, не path. Файл завершающий CR/LF теряет только line endings. Не коммитьте `.env`, реальные DSN, passwords, session/CSRF tokens или backups.

**Breaking config migration:** удалите `TARGET_WRITE_DATABASE_URL`; отдельный writer DSN не используется. Единый `TARGET_DATABASE_URL` сохраняется во всех режимах.

### Docker secrets

`docker-compose.secrets.yml` монтирует PostgreSQL password в admin-db, а admin DSN только в migrate/bootstrap/app. `docker-compose.target-secret.yml` отдельно добавляет target DSN только app. `docker-compose.bootstrap-secrets.yml` нужен только для первого admin. Секреты не копируются в image или frontend bundle.

Создайте локальные файлы без реальных значений в Git:

```powershell
New-Item -ItemType Directory -Path secrets -Force
# Запишите каждый secret в редакторе без командной строки:
notepad secrets\postgres_password
notepad secrets\admin_database_url
notepad secrets\bootstrap_admin_login
notepad secrets\bootstrap_admin_password
# Опционально: secrets\target_database_url
```

```bash
mkdir -p secrets
chmod 700 secrets
# Создайте файлы редактором; затем:
chmod 600 secrets/*
```

Каталоги `secrets/`, `backups/` и `*.dump` исключены из Git и Docker build context. Пример первого production secrets + TLS запуска:

```bash
docker compose -f docker-compose.yml -f docker-compose.secrets.yml -f docker-compose.bootstrap-secrets.yml -f docker-compose.target-secret.yml -f docker-compose.tls.yml up --build -d
```

Без target или для повторного запуска уберите соответствующий override. Для non-TLS замените TLS файл на `docker-compose.http.yml`. Обычный env-based local development не меняется.

### Backup admin database

Backup использует `pg_dump` custom format внутри admin-db и отдельный named volume `admin-db-backups`; password не передаётся аргументом, binary output не проходит через shell/PowerShell redirection. Полный admin backup включает users, roles/permissions, sessions, settings (если появятся), audit events и `goose_db_version`. Target DB в него **не входит** — её backup выполняет владелец target.

На работающем stack с выбранным `COMPOSE_FILE` или текущим base Compose:

```bash
sh scripts/backup-admin.sh
# Или явное имя:
sh scripts/backup-admin.sh velmir-admin-before-upgrade.dump
docker compose exec -T admin-db ls -l /backups
```

```powershell
.\scripts\Backup-Admin.ps1
# Или:
.\scripts\Backup-Admin.ps1 -BackupName velmir-admin-before-upgrade.dump
docker compose exec -T admin-db ls -l /backups
```

Для demo в PowerShell предварительно задайте `$env:COMPOSE_FILE = "docker-compose.yml;docker-compose.demo.yml"`, в shell — `COMPOSE_FILE=docker-compose.yml:docker-compose.demo.yml`. Не перенаправляйте `.dump` через `>`: это может повредить binary в PowerShell. Для внешнего архивирования используйте `docker cp`, а не text redirection. Backup files не попадают в Git/image.

### Restore и проверка

Предпочтителен fresh isolated PostgreSQL/admin DB и отдельный Compose project. Не восстанавливайте поверх production DB. Scripts ниже создают **новую** DB через `createdb` (существующее имя вызывает ошибку), затем `pg_restore --single-transaction --exit-on-error`; automatic destructive restore отсутствует.

```bash
sh scripts/restore-admin-fresh.sh velmir-admin-before-upgrade.dump velmir_admin_restored
```

```powershell
.\scripts\Restore-AdminFresh.ps1 -BackupName velmir-admin-before-upgrade.dump -NewDatabaseName velmir_admin_restored
```

Для полной изоляции запустите временный admin-db с отдельными project/volume и смонтируйте копию backup в `/backups`; выполните тот же restore там. Затем направьте отдельный app на restored `ADMIN_DATABASE_URL`, запустите migrate (идемпотентен) и проверьте вход существующего admin, `/api/auth/me`, roles/permissions и history. `TARGET_DATABASE_URL` для проверки restored admin state не нужен. SQL-контроль: users, `user_roles`, `role_permissions`, `audit_events`, `sessions` и последняя `goose_db_version` должны соответствовать backup. После проверки остановите только временные resources; не удаляйте существующие project volumes.

### Upgrade и rollback

1. Создайте и проверьте nonempty backup до обновления.
2. Обновите image и запустите отдельный migrate job при доступной admin DB:

```bash
docker compose -f docker-compose.yml -f docker-compose.http.yml up -d admin-db
docker compose -f docker-compose.yml -f docker-compose.http.yml run --rm migrate
docker compose -f docker-compose.yml -f docker-compose.http.yml run --rm migrate
docker compose -f docker-compose.yml -f docker-compose.http.yml up -d
```

Повторный migrate безопасен благодаря Goose state. Для TLS/secrets используйте тот же полный набор `-f`. Rollback image **не откатывает migrations**: старый image запускайте только если он совместим с новой schema. Нет automatic destructive migration rollback; восстановление backup — отдельная ручная disaster recovery операция в fresh DB с оценкой потери данных после backup.

### Несколько instances

Expired session cleanup безопасен на нескольких replicas благодаря batches и `SKIP LOCKED`; он затрагивает только admin DB и не удаляет active sessions. Login rate limiter остаётся **process-local** и **не синхронизируется** между replicas: общий лимит зависит от распределения запросов. Для единого внешнего limit используйте proxy отдельно. Sticky sessions не требуются, поскольку session records общие; background jobs не являются distributed scheduler.

### Локальная разработка Vite

```powershell
Copy-Item .env.example .env
docker compose -f docker-compose.yml -f docker-compose.demo.yml up -d admin-db target-db
Set-Location backend
go run ./cmd/migrate
# Задайте безопасные VELMIR_BOOTSTRAP_ADMIN_LOGIN/PASSWORD перед первым bootstrap.
go run ./cmd/bootstrap
go run ./cmd/server
```

Во втором терминале:

```powershell
Set-Location frontend
Copy-Item .env.example .env
npm ci
npm run dev
```

Открывайте Vite URL (обычно `http://localhost:5173`). Vite проксирует `/api`, `/healthz`, `/readyz`; cookies остаются same-origin. Production Go server сам отдаёт `dist`, hashed assets с long-term cache и `index.html` с `no-cache`; `/api/*` никогда не попадает в SPA fallback.

**H2 hardening:** добавлены optional Caddy HTTPS, explicit trusted proxy configuration, CSP/HSTS, Docker secrets, session maintenance и admin backup/restore. Автоматических GRANT/DDL в target нет; field-level permissions не реализованы и остаются следующим функциональным этапом.

## Дизайн и язык интерфейса

- Корпоративная палитра: Velmir Deep `#0F6B5F`, Midnight Bark `#101B20`, Source Mist `#F4F7F5` и Mimir Gold `#C18A3D`. Семантические цветовые токены, включая контрастные варианты для тёмных поверхностей и состояния error/success/disabled, централизованы в `frontend/src/style.css`.
- Исходные SVG и PNG находятся в `frontend/public/brand/`. Полный логотип используется на странице входа, compact — на боковой панели, mark — на узких экранах; light/dark варианты выбираются по фону. Форма знака не изменяется средствами CSS.
- SVG/PNG favicon, Apple touch icon и `site.webmanifest` подключены в `frontend/index.html`. Service worker не используется.
- Интерфейс доступен на русском и английском. По умолчанию используется русский; переключатель в общем footer меняет язык без перезагрузки и обновляет `html lang`. Явный выбор сохраняется только в `localStorage` под ключом `velmir_locale` (`ru` или `en`), backend для этого не требуется. Переводы сгруппированы в `frontend/src/locales/`, а английский является fallback для отсутствующих строк.

## Аутентификация и сессии

При запуске приложение вызывает `GET /api/auth/me`, перенаправляет неавторизованных пользователей на `/login`, а после входа восстанавливает исходный маршрут. Выход отзывает только текущую сессию. Для mutation-запросов и выхода требуется проверяемый сервером заголовок `X-CSRF-Token`. Пароли, session tokens и CSRF tokens не сохраняются в `localStorage`; session cookie имеет флаг HttpOnly, а CSRF token хранится только в памяти.

Session cookies используют `SameSite=Lax`; idle expiration по умолчанию составляет `30m`, absolute expiration — `24h`. Expired records удаляются фоновыми batches каждые `15m` (настраивается); active sessions не затрагиваются, а SIGTERM останавливает cleanup. Атрибуты cookies, настройки и ответы endpoints описаны в `docs/api.md`.

## Роли, права и управление пользователями

Доступ по умолчанию запрещён. Роли можно комбинировать: effective permissions пользователя являются объединением прав всех назначенных ролей. Встроенная роль `admin` разрешает чтение, создание, обновление и удаление всех ресурсов; `viewer` разрешает только чтение. Права `read`, `create`, `update` и `delete` могут быть привязаны к конкретной схеме и ресурсу. `delete` является отдельным правом и не следует из остальных прав или доступности writer.

Bootstrap-администратор получает `admin` автоматически. Новый пользователь не получает роль автоматически и имеет доступ только к `/api/auth/me` с пустым списком permissions; защищённые ресурсы возвращают 403. Backend фильтрует `/api/schema` по `read`, а frontend скрывает недоступные ресурсы и create/update/delete controls. Для viewer отображается read-only состояние.

Администратору доступен отдельный раздел **Administration**: создание пользователей, смена display name, включение/отключение, сброс пароля и назначение ролей, а также создание и редактирование пользовательских ролей и их permissions. Раздел скрыт от viewer и пользователей без роли, но backend независимо проверяет системную роль `admin`, session и CSRF. Отключение и сброс пароля отзывают все сессии пользователя. Запрещены самоотключение, снятие собственной роли `admin` и удаление admin-прав у последнего активного администратора. Системные роли не редактируются; wildcard `*.*` и конкретные schema/resource поддерживают `read`, `create`, `update`, `delete`. Пользовательская роль получает `delete` только после явного назначения в PermissionEditor; миграция не добавляет его существующим custom roles. Изменения применяются к следующим запросам без перезапуска. Полные API-контракты приведены в `docs/api.md`.

CLI `cmd/users` сохраняется для локального администрирования и восстановления доступа. После применения миграций используйте команды из каталога `backend`:

```powershell
# Создание пользователя; пароль не передаётся аргументом командной строки.
$env:VELMIR_USER_PASSWORD = "<long-random-password>"
go run ./cmd/users create -login "viewer@example.test" -display-name "Viewer"
Remove-Item Env:VELMIR_USER_PASSWORD

# Назначение роли и просмотр пользователей.
go run ./cmd/users assign-role -login "viewer@example.test" -role viewer
go run ./cmd/users list
```

Для локальной проверки viewer эти две команды создают учётную запись и назначают ей роль `viewer`. Войдите под ней в отдельном браузерном профиле: schema и records GET доступны, а кнопки создания и редактирования скрыты. Повторное `assign-role` идемпотентно; для назначения дополнительной роли выполните команду ещё раз с её именем. Пароль должен быть длиной не менее 12 символов, передаётся только через `VELMIR_USER_PASSWORD` и сохраняется исключительно как Argon2id hash. Не помещайте реальные пароли в документацию или историю команд.

## Базы данных и безопасность записи

- **Admin database** (`velmir_admin`) принадлежит Velmir. Отдельный migrate CLI применяет Goose-миграции только здесь и создаёт `users`, `sessions`, `roles`, `user_roles`, `role_permissions` и `audit_events`.
- **Demo target database** (`velmir_demo`) — отдельный PostgreSQL, созданный только demo override. Существующие initialization fixtures создают демонстрационные таблицы, записи, представление, PK/FK и локальные роли; последующих автоматических GRANT нет.
- `TARGET_DATABASE_URL` — единый optional DSN. Из него создаются два пула: read pool всегда устанавливает `default_transaction_read_only=on`, mutation pool используется только поддерживаемыми INSERT/UPDATE/DELETE после RBAC/CSRF checks.
- `write_enabled` зависит от настроенного target и effective Velmir permissions. Фактическая возможность mutation также ограничена privileges PostgreSQL role; viewer остаётся read-only через Velmir authorization даже с полноправным DSN.
- Velmir никогда не выполняет миграции, автоматические GRANT, DDL или arbitrary SQL в target database. Не указывайте integration-тесты или target URL на реальную базу для локальной demo настройки.

Для полного CRUD владелец target database заранее выдаёт application role только необходимые privileges: `CONNECT` на database, `USAGE` на используемые schemas, `SELECT` для чтения и `INSERT`, `UPDATE`, `DELETE` на разрешённые таблицы, а также `USAGE`, `SELECT`, `UPDATE` на sequences, необходимые для default/identity values. Для read-only role достаточно `CONNECT`, schema `USAGE` и `SELECT`. Права `CREATE`, `ALTER`, `DROP`, `TRUNCATE` приложению не нужны. Velmir сам их не выдаёт.

Без `TARGET_DATABASE_URL` backend запускается, readiness показывает `not_configured`, а `/api/schema` возвращает `TARGET_NOT_CONFIGURED`. Configured but unavailable target явно показывается как `unavailable` и делает `/readyz` неуспешным; `/healthz` остаётся независимым liveness.

## Создание, редактирование и удаление записей

Frontend строит все формы на основе `/api/schema`; форм для конкретных таблиц нет. Поддерживаются text, boolean, integer, bigint/numeric, date, timestamps, UUID, enums и JSON. Для nullable-полей есть отдельный NULL control, а в формах создания можно явно использовать database default. Сгенерированные поля и `always` identity не являются обязательными и не редактируются.

Для обновления и удаления требуется первичный ключ, в том числе составной; поля первичного ключа изменять нельзя. В таблицах без первичного ключа можно создавать записи, но нельзя обновлять или удалять. Представления доступны только для чтения. Неподдерживаемые типы PostgreSQL отображаются, но автоматически не редактируются. Нарушения PostgreSQL not-null, unique, foreign-key и check отображаются как ошибки API/формы.

### Many-to-one relation selectors

Для доступных по `read` referenced resources формы generic-образом заменяют raw FK inputs на selectors по metadata `/api/schema`. Simple FK использует один selector, composite FK — один логический selector для всех local/referenced columns. Загружается только первая страница по 20 записей; поиск выполняется на server с debounce 300 ms, следующие страницы добавляются через **Загрузить ещё**. Текущее значение при редактировании отдельно получается точными typed filters по всем referenced columns и не исчезает, если его нет на первой странице.

Label выбирается в порядке `display_name`, `name`, `title`, `label`, другое непустое текстовое поле, затем technical identifier; рядом показываются все части referenced key. Selector лишь заполняет существующие FK columns в прежнем POST/PATCH payload. Nullable relation можно очистить целиком, required — нельзя; частично заполненный composite FK не отправляется. Bigint/numeric и UUID сохраняют строковое представление, NULL остаётся JSON `null`.

Без `read` на referenced resource related records и labels не запрашиваются: остаётся безопасный raw FK input, а доступная FK metadata не раскрывает скрытый ресурс. Read-only controls не позволяют менять связь. Nested mutations и автоматическое создание/редактирование связанных записей отсутствуют.

### Related labels и one-to-many panels

В списках записей many-to-one FK отображаются через общий label helper: `display_name`, `name`, `title`, `label`, другое текстовое поле, затем technical identifier. Рядом сохраняются referenced key и все части composite key; NULL имеет локализованное состояние, а при ошибке остаётся raw FK. Read-only `POST /api/resources/{schema}/{resource}/relations/resolve` группирует дедуплицированные ключи по relations и не требует CSRF, writer или audit. Лимиты — 256 КиБ, 20 relations, 200 keys на relation и 1000 суммарно. Per-row запросы не выполняются.

`/api/schema` дополнительно возвращает RBAC-filtered `reverse_relations` с constraint name, child schema/resource и упорядоченными local/referenced columns. На parent form/detail они открывают generic read-only child panels с typed filters по всем частям simple/composite key, отдельной pagination и независимыми loading/empty/401/403/error states. Detail доступен по `read` без save controls; reverse relations с типами, для которых records API не имеет `eq` filter, пока скрываются без запроса. Несохранённый parent показывает **Сначала сохраните запись** и не запускает child query. Панели не содержат create/edit/delete controls и не сбрасывают parent values или pagination при смене языка.

Права проверяются backend и frontend независимо: read только на parent скрывает labels/panels; read на parent и child разрешает их; read только на referenced resource не открывает resolve для недоступного current resource. Пользователь без session получает 401, authenticated без permission — 403 без login redirect. Подробный batch contract, reverse metadata и permission matrix приведены в `docs/api.md`. Создание дочерних записей из parent form и nested mutations не реализованы.

### Many-to-many editor через junction tables

Parent form/detail generic-образом показывает target records через поддерживаемую PostgreSQL junction table. Detection использует только RBAC-filtered `/api/schema`: нужны ровно два many-to-one FK к разным ресурсам и composite PK ровно из колонок обоих FK; дополнительные поля должны быть nullable или иметь default/generated/identity values. Self-referential relations, surrogate PK, три и более FK и обязательные дополнительные junction attributes без default/generated не поддерживаются и остаются обычными ресурсами/reverse panels.

Для панели нужны `read` на parent, target и junction. Записи загружаются существующим records list с typed parent filters и pagination, labels — одним batch relations/resolve на страницу. **Добавить связь** требует writer, `create` на junction и `read` на target, использует selector с server-side search и обычный flat junction POST. **Удалить связь** требует writer и `delete` на junction, использует DELETE по полному composite junction PK. Её confirmation прямо сообщает, что удаляется только junction record, а parent/target records остаются без изменений; это не обычное **Удалить запись**. Права `update` на parent/target не нужны.

Создание и удаление связи создают обычные audit `create`/`delete` только для junction resource. Duplicate/failed mutations не аудируются; после `AUDIT_WRITE_FAILED` committed связь не повторяется автоматически, список обновляется с отдельным предупреждением. Editor не поддерживает nested mutations, создание связанных записей или редактирование дополнительных junction attributes. Подробности и полная permission matrix приведены в `docs/api.md`.

После сохранения текущий список записей обновляется. Формы предупреждают о несохранённых изменениях перед закрытием или уходом со страницы. При конкурентных обновлениях сейчас действует семантика **last write wins**; optimistic locking и обнаружение конфликтов не реализованы.

Кнопка **Удалить** доступна только при праве `delete`, настроенном writer и таблице с PK. Accessible dialog показывает resource и точный simple/composite record identifier, предупреждает о необратимости и блокирует повторную отправку. После 204 список обновляется; при удалении последней записи страницы выполняется переход на предыдущую страницу. Bulk delete отсутствует: один запрос удаляет ровно одну запись.

Тела mutation-запросов, кодирование ключей записей и коды ошибок описаны в `docs/api.md`.

## История создания, редактирования и удаления

Каждый успешный POST/PATCH/DELETE через Velmir создаёт в admin database событие `audit_events` с UUID, серверным timestamp, фактическим actor, действием `create`/`update`/`delete`, schema/resource, простым или составным record key и безопасными before/after JSONB. Для create сохраняется after, для update — before и after, для delete — фактическое before из `DELETE … RETURNING` и `after_data IS NULL`. Пароли, tokens, secrets и connection strings исключаются. Bigint/numeric остаются строками, bytea — Base64, JSON/JSONB сохраняет точность на backend.

Для строки с первичным ключом кнопка **История** открывает generic-панель с actor, временем через `Intl`, действием и значениями. Поддерживаются ru/en без перезагрузки, пагинация и мобильная раскладка. API `GET /api/resources/{schema}/{resource}/records/{key}/history` требует session и `read`; без session возвращается 401, без read — 403 без перенаправления на login. История удалённой записи остаётся доступной по её key: существование target-записи не проверяется. Истории внешних изменений вне Velmir нет.

Target mutation сначала commit в target database, затем audit пишется в admin database: межбазовой атомарности нет. Если audit не удался после commit, API возвращает `AUDIT_WRITE_FAILED` и `mutation_committed=true`, frontend сообщает, что изменение уже применено, и обновляет данные. Для DELETE показывается отдельное предупреждение: запись уже удалена, но событие истории могло не записаться. Автоматического retry POST/PATCH/DELETE и компенсирующих DELETE/UPDATE/INSERT нет. Подробная схема, контракт history и ограничения приведены в `docs/api.md`.

## Сохранность demo target

Initialization fixtures demo target выполняются только при создании нового named volume. Для сохранения admin/demo данных используйте обычные `stop`, `down` и повторный `up`; удаление volumes в H1 не требуется.

## Проверки

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

Integration-тесты исключены из unit-тестов с помощью build tag `integration`. Запускайте их отдельно и только с инициализированной локальной демонстрационной базой:

```powershell
Set-Location backend
$env:TEST_TARGET_DATABASE_URL = "postgresql://velmir_writer:velmir_writer_local_only@localhost:5433/velmir_demo?sslmode=disable"
go test -tags=integration ./internal/introspection -v -timeout 30s
go test -tags=integration ./internal/records -v -timeout 30s

$env:TEST_ADMIN_DATABASE_URL = "postgresql://velmir:velmir_local_only@localhost:5432/velmir_admin?sslmode=disable"
go test -tags=integration ./internal/auth -v -timeout 30s
go test -tags=integration ./internal/httpapi -v -timeout 60s

# Полный прогон с уже заданными двумя локальными test URL.
go test -tags=integration ./... -v -timeout 60s
Remove-Item Env:TEST_ADMIN_DATABASE_URL
Remove-Item Env:TEST_TARGET_DATABASE_URL
```

Integration-тесты writer и history создают, обновляют и удаляют демонстрационные записи, включая simple/composite PK, FK errors и audit failure после commit. Некоторые записи для проверки сбоя audit намеренно остаются в demo database. Тесты отказываются работать с нелокальным хостом или базой, отличной от `velmir_demo`.

Integration-тесты auth, audit storage и admin/history API требуют локальную базу `velmir_admin` с применёнными миграциями, отклоняют нелокальные хосты и другие имена баз и откатывают admin-транзакции. Не запускайте их против реальной admin database. Target-схема тестами не изменяется.

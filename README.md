# Velmir

[English version](README_EN.md)

Velmir — панель администрирования PostgreSQL, управляемая метаданными. На текущем этапе реализованы introspection схемы, универсальные списки записей с пагинацией, сортировкой, поиском и типизированными фильтрами, автоматические формы создания и редактирования записей, история создания и редактирования через Velmir, аутентификация с серверными сессиями, RBAC и управление пользователями и ролями через UI. Удаление записей, relations UI, файлы и автоматизация пока не реализованы.

## Требования

- Go **1.26.0** (см. `backend/go.mod`)
- Node.js и npm
- Docker с Docker Compose

## Локальный запуск

В корне репозитория в PowerShell:

```powershell
Copy-Item .env.example .env
docker compose up -d admin-db target-db
```

Перед первым запуском backend задайте в `.env` переменные `VELMIR_BOOTSTRAP_ADMIN_EMAIL` и `VELMIR_BOOTSTRAP_ADMIN_PASSWORD`: уникальный email администратора и длинный случайный пароль длиной не менее 12 символов. Не используйте стандартный пароль. Примените миграции к admin database и запустите backend:

```powershell
Set-Location backend
go run ./cmd/migrate
go run ./cmd/server
```

Если в admin database нет пользователей, при запуске сервер однократно создаёт первого администратора и выводит в лог напоминание без пароля. **После создания администратора удалите обе bootstrap-переменные из `.env` и окружения процесса, затем перезапустите backend.** При последующих запусках новый пользователь не создаётся, пока в базе существует хотя бы один пользователь. Если база пуста, а bootstrap-значения не заданы, запуск завершится с понятной ошибкой. Публичная регистрация отсутствует; последующие учётные записи создаёт администратор через UI или CLI.

Во втором терминале запустите frontend:

```powershell
Set-Location frontend
Copy-Item .env.example .env
npm ci
npm run dev
```

По умолчанию backend слушает `http://127.0.0.1:8080` и остаётся доступным только локально, пока явно не изменён `HTTP_ADDR`. Открывайте URL Vite (обычно `http://localhost:5173`), а не URL backend. Vite проксирует `/api`, `/healthz` и `/readyz` на `VITE_BACKEND_PROXY_TARGET`, поэтому cookies и API-запросы используют тот же origin в браузере. `VITE_BACKEND_BASE_URL` должен оставаться пустым.

В production раздавайте собранный frontend и backend API через один HTTPS origin и настройте SPA fallback для `/login` и `/administration`. Оставьте `VITE_BACKEND_BASE_URL` пустым и установите `SESSION_COOKIE_SECURE=true`. CORS wildcard с credentials не используется и не требуется.

## Дизайн и язык интерфейса

- Корпоративная палитра: Velmir Deep `#0F6B5F`, Midnight Bark `#101B20`, Source Mist `#F4F7F5` и Mimir Gold `#C18A3D`. Семантические цветовые токены, включая контрастные варианты для тёмных поверхностей и состояния error/success/disabled, централизованы в `frontend/src/style.css`.
- Исходные SVG и PNG находятся в `frontend/public/brand/`. Полный логотип используется на странице входа, compact — на боковой панели, mark — на узких экранах; light/dark варианты выбираются по фону. Форма знака не изменяется средствами CSS.
- SVG/PNG favicon, Apple touch icon и `site.webmanifest` подключены в `frontend/index.html`. Service worker не используется.
- Интерфейс доступен на русском и английском. По умолчанию используется русский; переключатель в общем footer меняет язык без перезагрузки и обновляет `html lang`. Явный выбор сохраняется только в `localStorage` под ключом `velmir_locale` (`ru` или `en`), backend для этого не требуется. Переводы сгруппированы в `frontend/src/locales/`, а английский является fallback для отсутствующих строк.

## Аутентификация и сессии

При запуске приложение вызывает `GET /api/auth/me`, перенаправляет неавторизованных пользователей на `/login`, а после входа восстанавливает исходный маршрут. Выход отзывает только текущую сессию. Для mutation-запросов и выхода требуется проверяемый сервером заголовок `X-CSRF-Token`. Пароли, session tokens и CSRF tokens не сохраняются в `localStorage`; session cookie имеет флаг HttpOnly, а CSRF token хранится только в памяти.

Session cookies используют `SameSite=Lax`; idle expiration по умолчанию составляет `30m`, absolute expiration — `24h`. Атрибуты cookies, настройки и ответы endpoints описаны в `docs/api.md`.

## Роли, права и управление пользователями

Доступ по умолчанию запрещён. Роли можно комбинировать: effective permissions пользователя являются объединением прав всех назначенных ролей. Встроенная роль `admin` разрешает чтение, создание и обновление всех ресурсов; `viewer` разрешает только чтение. Права `read`, `create` и `update` могут быть привязаны к конкретной схеме и ресурсу. DELETE отсутствует у всех ролей, включая `admin`.

Bootstrap-администратор получает `admin` автоматически. Новый пользователь не получает роль автоматически и имеет доступ только к `/api/auth/me` с пустым списком permissions; защищённые ресурсы возвращают 403. Backend фильтрует `/api/schema` по `read`, а frontend скрывает недоступные ресурсы и create/update controls. Для viewer отображается read-only состояние.

Администратору доступен отдельный раздел **Administration**: создание пользователей, смена display name, включение/отключение, сброс пароля и назначение ролей, а также создание и редактирование пользовательских ролей и их permissions. Раздел скрыт от viewer и пользователей без роли, но backend независимо проверяет системную роль `admin`, session и CSRF. Отключение и сброс пароля отзывают все сессии пользователя. Запрещены самоотключение, снятие собственной роли `admin` и удаление admin-прав у последнего активного администратора. Системные роли не редактируются; wildcard `*.*` и конкретные schema/resource поддерживают только `read`, `create`, `update`. Изменения применяются к следующим запросам без перезапуска. Полные API-контракты приведены в `docs/api.md`.

CLI `cmd/users` сохраняется для локального администрирования и восстановления доступа. После применения миграций используйте команды из каталога `backend`:

```powershell
# Создание пользователя; пароль не передаётся аргументом командной строки.
$env:VELMIR_USER_PASSWORD = "<long-random-password>"
go run ./cmd/users create -email "viewer@example.test" -display-name "Viewer"
Remove-Item Env:VELMIR_USER_PASSWORD

# Назначение роли и просмотр пользователей.
go run ./cmd/users assign-role -email "viewer@example.test" -role viewer
go run ./cmd/users list
```

Для локальной проверки viewer эти две команды создают учётную запись и назначают ей роль `viewer`. Войдите под ней в отдельном браузерном профиле: schema и records GET доступны, а кнопки создания и редактирования скрыты. Повторное `assign-role` идемпотентно; для назначения дополнительной роли выполните команду ещё раз с её именем. Пароль должен быть длиной не менее 12 символов, передаётся только через `VELMIR_USER_PASSWORD` и сохраняется исключительно как Argon2id hash. Не помещайте реальные пароли в документацию или историю команд.

## Базы данных и безопасность записи

- **Admin database** (`localhost:5432`, база `velmir_admin`) принадлежит Velmir. Goose-миграции выполняются только здесь и создают migration baseline, таблицы `users`, `sessions`, `roles`, `user_roles`, `role_permissions` и `audit_events`.
- **Демонстрационная target database** (`localhost:5433`, база `velmir_demo`) — отдельный экземпляр PostgreSQL. Docker создаёт демонстрационные таблицы, записи, представление, первичные и внешние ключи.
- `TARGET_DATABASE_URL` — существующее подключение к target database только для чтения. Его пул всегда устанавливает `default_transaction_read_only=on`; он используется для introspection и чтения записей и никогда — для mutations.
- `TARGET_WRITE_DATABASE_URL` — отдельное **необязательное** подключение, используемое только для INSERT и UPDATE. Пример указывает на локальную демонстрационную writer-роль. Оставьте переменную пустой или удалите её, чтобы отключить элементы управления mutations и получать `TARGET_READ_ONLY` от mutation endpoints.
- Velmir никогда не выполняет миграции, автоматические GRANT или DDL в target database. Не указывайте в integration-тестах или target URL реальную базу данных при локальной демонстрационной настройке.

Демонстрационная роль `velmir_writer` имеет только:

- `CONNECT` к `velmir_demo` и `USAGE` для схемы `public`;
- `SELECT`, `INSERT`, `UPDATE` для существующих демонстрационных таблиц;
- `USAGE`, `SELECT`, `UPDATE` для существующих демонстрационных последовательностей, необходимых для identity/default значений.

У неё нет прав `CREATE`, `ALTER`, `DROP`, `TRUNCATE` или `DELETE`. Роль `velmir_readonly` сохраняет read-only транзакции и права SELECT.

Чтобы запустить приложение без target database, оставьте `TARGET_DATABASE_URL` пустым. Backend всё равно запустится, readiness покажет `not_configured`, а `/api/schema` вернёт `TARGET_NOT_CONFIGURED`.

## Создание и редактирование записей

Frontend строит все формы на основе `/api/schema`; форм для конкретных таблиц нет. Поддерживаются text, boolean, integer, bigint/numeric, date, timestamps, UUID, enums и JSON. Для nullable-полей есть отдельный NULL control, а в формах создания можно явно использовать database default. Сгенерированные поля и `always` identity не являются обязательными и не редактируются.

Для обновления требуется первичный ключ, в том числе составной; поля первичного ключа изменять нельзя. В таблицах без первичного ключа можно создавать записи, но нельзя обновлять. Представления доступны только для чтения. Неподдерживаемые типы PostgreSQL отображаются, но автоматически не редактируются. Нарушения PostgreSQL not-null, unique, foreign-key и check отображаются как ошибки API/формы.

После сохранения текущий список записей обновляется. Формы предупреждают о несохранённых изменениях перед закрытием или уходом со страницы. При конкурентных обновлениях сейчас действует семантика **last write wins**; optimistic locking и обнаружение конфликтов не реализованы. Удаление намеренно недоступно.

Тела mutation-запросов, кодирование ключей записей и коды ошибок описаны в `docs/api.md`.

## История создания и редактирования

Каждый успешный POST/PATCH через Velmir создаёт в admin database событие `audit_events` с UUID, серверным timestamp, фактическим actor, действием `create`/`update`, schema/resource, простым или составным record key и безопасными before/after JSONB. Для create сохраняется after, для update — before и after из фактического состояния target database. Пароли, tokens, secrets и connection strings исключаются. Bigint/numeric остаются строками, bytea — Base64, JSON/JSONB сохраняет точность на backend.

Для строки с первичным ключом кнопка **История** открывает generic-панель с actor, временем через `Intl`, действием и изменёнными полями. Поддерживаются ru/en без перезагрузки, пагинация и мобильная раскладка. API `GET /api/resources/{schema}/{resource}/records/{key}/history` требует session и `read`; без session возвращается 401, без read — 403 без перенаправления на login. Истории внешних изменений вне Velmir и DELETE на этом этапе нет.

Target mutation сначала commit в target database, затем audit пишется в admin database: межбазовой атомарности нет. Если audit не удался после commit, API возвращает `AUDIT_WRITE_FAILED` и `mutation_committed=true`, frontend сообщает, что изменение уже сохранено, и предлагает обновить данные. Автоматического retry POST/PATCH и компенсирующих DELETE/UPDATE нет. Подробная схема, контракт history и ограничения приведены в `docs/api.md`.

## Сброс демонстрационной target database

Скрипты инициализации выполняются только при создании Docker нового тома target database. На текущем этапе добавлены демонстрационная writer-роль и столбец `book_categories.note`, поэтому уже инициализированный том нужно пересоздать для их применения.

Следующие команды удаляют **только локальный контейнер target `velmir_demo` и его демонстрационный том**. Они не удаляют том admin database. Не используйте их для реальной базы данных:

```powershell
docker compose stop target-db
docker compose rm -f target-db
docker volume rm velmir_target-db-data
docker compose up -d target-db
```

Если имя Compose-проекта отличается от `velmir`, замените префикс тома на имя, показанное командой `docker volume ls`.

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
$env:TEST_TARGET_DATABASE_URL = "postgresql://velmir_readonly:velmir_readonly_local_only@localhost:5433/velmir_demo?sslmode=disable"
go test -tags=integration ./internal/introspection -v -timeout 30s
go test -tags=integration ./internal/records -v -timeout 30s

$env:TEST_TARGET_WRITE_DATABASE_URL = "postgresql://velmir_writer:velmir_writer_local_only@localhost:5433/velmir_demo?sslmode=disable"
go test -tags=integration ./internal/records -run TestWriterDemoDatabase -v -timeout 30s

$env:TEST_ADMIN_DATABASE_URL = "postgresql://velmir:velmir_local_only@localhost:5432/velmir_admin?sslmode=disable"
go test -tags=integration ./internal/auth -v -timeout 30s
go test -tags=integration ./internal/httpapi -v -timeout 60s

# Полный прогон с уже заданными тремя локальными test URL.
go test -tags=integration ./... -v -timeout 60s
Remove-Item Env:TEST_ADMIN_DATABASE_URL
Remove-Item Env:TEST_TARGET_DATABASE_URL
Remove-Item Env:TEST_TARGET_WRITE_DATABASE_URL
```

Integration-тесты writer и history создают и обновляют демонстрационные записи и намеренно оставляют их в демонстрационной базе; записи не удаляются. Тесты отказываются работать с нелокальным хостом или базой, отличной от `velmir_demo`.

Integration-тесты auth, audit storage и admin/history API требуют локальную базу `velmir_admin` с применёнными миграциями, отклоняют нелокальные хосты и другие имена баз и откатывают admin-транзакции. Не запускайте их против реальной admin database. Target-схема тестами не изменяется.

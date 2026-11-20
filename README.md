# Velmir

[English version](README_EN.md)

Velmir — панель администрирования PostgreSQL, управляемая метаданными. На текущем этапе реализованы introspection схемы, универсальные списки записей с пагинацией, сортировкой, поиском и типизированными фильтрами, related labels, автоматические формы создания и редактирования с many-to-one relation selectors, read-only one-to-many panels и базовый many-to-many editor через junction tables, удаление записей с подтверждением, история изменений через Velmir, аутентификация с серверными сессиями, RBAC и управление пользователями и ролями через UI. Nested mutations, файлы и автоматизация пока не реализованы.

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

Доступ по умолчанию запрещён. Роли можно комбинировать: effective permissions пользователя являются объединением прав всех назначенных ролей. Встроенная роль `admin` разрешает чтение, создание, обновление и удаление всех ресурсов; `viewer` разрешает только чтение. Права `read`, `create`, `update` и `delete` могут быть привязаны к конкретной схеме и ресурсу. `delete` является отдельным правом и не следует из остальных прав или доступности writer.

Bootstrap-администратор получает `admin` автоматически. Новый пользователь не получает роль автоматически и имеет доступ только к `/api/auth/me` с пустым списком permissions; защищённые ресурсы возвращают 403. Backend фильтрует `/api/schema` по `read`, а frontend скрывает недоступные ресурсы и create/update/delete controls. Для viewer отображается read-only состояние.

Администратору доступен отдельный раздел **Administration**: создание пользователей, смена display name, включение/отключение, сброс пароля и назначение ролей, а также создание и редактирование пользовательских ролей и их permissions. Раздел скрыт от viewer и пользователей без роли, но backend независимо проверяет системную роль `admin`, session и CSRF. Отключение и сброс пароля отзывают все сессии пользователя. Запрещены самоотключение, снятие собственной роли `admin` и удаление admin-прав у последнего активного администратора. Системные роли не редактируются; wildcard `*.*` и конкретные schema/resource поддерживают `read`, `create`, `update`, `delete`. Пользовательская роль получает `delete` только после явного назначения в PermissionEditor; миграция не добавляет его существующим custom roles. Изменения применяются к следующим запросам без перезапуска. Полные API-контракты приведены в `docs/api.md`.

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
- `TARGET_WRITE_DATABASE_URL` — отдельное **необязательное** подключение, используемое только для INSERT, UPDATE и DELETE. Пример указывает на локальную демонстрационную writer-роль. Оставьте переменную пустой или удалите её, чтобы отключить элементы управления mutations и получать `TARGET_READ_ONLY` от mutation endpoints.
- Velmir никогда не выполняет миграции, автоматические GRANT или DDL в target database. Не указывайте в integration-тестах или target URL реальную базу данных при локальной демонстрационной настройке.

Демонстрационная роль `velmir_writer` имеет только:

- `CONNECT` к `velmir_demo` и `USAGE` для схемы `public`;
- `SELECT`, `INSERT`, `UPDATE`, `DELETE` для существующих демонстрационных таблиц;
- `USAGE`, `SELECT`, `UPDATE` для существующих демонстрационных последовательностей, необходимых для identity/default значений.

У неё нет прав `CREATE`, `ALTER`, `DROP` или `TRUNCATE`. Роль `velmir_readonly` сохраняет read-only транзакции и права SELECT. Для уже инициализированной **только локальной** `velmir_demo` владелец может однократно выдать DELETE без пересоздания тома:

```powershell
docker compose exec target-db psql -U velmir_target_admin -d velmir_demo -v ON_ERROR_STOP=1 -c "GRANT DELETE ON ALL TABLES IN SCHEMA public TO velmir_writer;"
```

Не выполняйте эту команду против реальной базы. В production необходимые privileges настраивает владелец базы вручную; приложение не выдаёт их автоматически.

Чтобы запустить приложение без target database, оставьте `TARGET_DATABASE_URL` пустым. Backend всё равно запустится, readiness покажет `not_configured`, а `/api/schema` вернёт `TARGET_NOT_CONFIGURED`.

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

## Сброс демонстрационной target database

Скрипты инициализации выполняются только при создании Docker нового тома target database. Для DELETE на уже инициализированном локальном томе достаточно одноразового GRANT из раздела «Базы данных и безопасность записи»; пересоздание тома не требуется.

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

Integration-тесты writer и history создают, обновляют и удаляют демонстрационные записи, включая simple/composite PK, FK errors и audit failure после commit. Некоторые записи для проверки сбоя audit намеренно остаются в demo database. Тесты отказываются работать с нелокальным хостом или базой, отличной от `velmir_demo`.

Integration-тесты auth, audit storage и admin/history API требуют локальную базу `velmir_admin` с применёнными миграциями, отклоняют нелокальные хосты и другие имена баз и откатывают admin-транзакции. Не запускайте их против реальной admin database. Target-схема тестами не изменяется.

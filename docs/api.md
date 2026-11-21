# API

[English version](api_en.md)

## Аутентификация и авторизация

`POST /api/auth/login` доступен публично. Все остальные endpoints `/api`, включая чтение схемы и записей, требуют действительной серверной сессии. `/healthz` и `/readyz` остаются публичными для инфраструктурных проверок. Неаутентифицированные запросы получают HTTP 401 с ошибкой в JSON-обёртке:

```json
{
  "error": {
    "code": "UNAUTHENTICATED",
    "message": "Authentication required"
  }
}
```

Доступ по умолчанию запрещён и проверяется на backend независимо от frontend. Пользователю можно назначить несколько ролей; их права объединяются. Системная роль `admin` даёт `read`, `create`, `update` и `delete` для всех ресурсов, а `viewer` — только `read`. Resource-level grant задаётся схемой, ресурсом и операцией; `*` в системных ролях означает все схемы и ресурсы. Неизвестные роли и отсутствие ролей не дают дополнительных прав. Право `delete` не следует из `read`, `create`, `update` или `write_enabled` и проверяется отдельно.

Аутентифицированный пользователь без необходимого права получает HTTP 403 с кодом `FORBIDDEN`; это отличается от HTTP 401 при отсутствии сессии и от `CSRF_TOKEN_INVALID`. `GET /api/auth/me` остаётся доступным и возвращает пустой список permissions пользователю без роли. Публичная регистрация недоступна. Управление пользователями и ролями разрешено только системной роли `admin`.

### POST /api/auth/login

Тело запроса:

```json
{
  "email": "admin@example.com",
  "password": "the-user-supplied-password"
}
```

Успешный ответ — HTTP 200:

```json
{
  "user": {
    "id": 1,
    "email": "admin@example.com",
    "display_name": "Administrator"
  },
  "csrf_token": "a-random-token-returned-by-the-server",
  "can_admin": true,
  "permissions": [
    {
      "schema": "*",
      "resource": "*",
      "operations": ["read", "create", "update", "delete"]
    }
  ]
}
```

Каждый вход создаёт новую сессию. Пароли проверяются с помощью Argon2id. Session tokens содержат 32 криптографически случайных байта, а в admin database сохраняются только их SHA-256 hashes. JWT не используются.

Неизвестный email, неправильный пароль и неактивная учётная запись возвращают одинаковый HTTP 401 с кодом `INVALID_CREDENTIALS` и сообщением `Invalid email or password`. Повторные неудачные попытки ограничиваются по исходному IP и по паре IP/email; HTTP 429 использует код `AUTH_RATE_LIMITED` без раскрытия информации об учётной записи.

### GET /api/auth/me

Возвращает HTTP 200 с текущим пользователем, CSRF token и effective permissions в том же формате, что и login. Поле `can_admin` истинно только при назначенной системной роли `admin`; wildcard permissions пользовательской роли не включают администрирование. Каждый элемент permissions содержит `schema`, `resource` и массив `operations`; повторяющиеся права нескольких ролей уже объединены. Пользователь без роли получает `"permissions": []` и `"can_admin": false`. Endpoint продлевает idle expiration сессии. Отсутствующий CSRF cookie заменяется новым. HTTP 401 означает, что сессия отсутствует, отозвана, истекла по idle timeout или по absolute timeout.

### POST /api/auth/logout

Требует session cookie и действительный CSRF-заголовок. Отзывает только текущую сессию, очищает оба authentication cookies и возвращает HTTP 204 без тела ответа.

### Cookies и CSRF

- `velmir_session` — session cookie с `Path=/`, `HttpOnly` и `SameSite=Lax`. Содержит случайный session token и никогда не доступен JavaScript.
- `velmir_csrf` имеет `Path=/` и `SameSite=Lax`, но не HttpOnly. Его значение также возвращается login/me и хранится только в памяти frontend.
- Оба cookies получают флаг `Secure` при `SESSION_COOKIE_SECURE=true`; для production с HTTPS этот параметр необходимо включить.
- Session cookie не имеет постоянного срока действия в браузере. Серверный idle expiration по умолчанию составляет 30 минут, absolute expiration — 24 часа. Они настраиваются через `SESSION_IDLE_TIMEOUT` и `SESSION_ABSOLUTE_TIMEOUT` с использованием синтаксиса длительностей Go.

Для всех POST/PATCH/PUT/DELETE-запросов и logout передавайте CSRF-значение из login/me в `X-CSRF-Token`. Сервер сравнивает его с `velmir_csrf` за постоянное время. Отсутствующее или несовпадающее значение возвращает HTTP 403 с кодом `CSRF_TOKEN_INVALID`. Login является неаутентифицированной точкой входа и не требует CSRF. Frontend использует same-origin запросы; CORS wildcard с credentials не поддерживается.

## Администрирование пользователей и ролей

Все endpoints ниже требуют session cookie и системную роль `admin`. Все mutations дополнительно требуют CSRF. Viewer, пользователь без роли и пользовательская роль с `*.*` получают 403 `FORBIDDEN`. Данные читаются и изменяются только в admin database. Ответы не содержат password hashes, session tokens или token hashes.

| Метод | Путь | Назначение и тело |
|---|---|---|
| GET | `/api/admin/users` | `{"users": [...]}`; пользователь содержит `id`, `email`, `display_name`, `is_active`, `roles` |
| POST | `/api/admin/users` | Создать активного пользователя без ролей: `email`, `display_name`, `password`; HTTP 201 |
| PATCH | `/api/admin/users/{id}/display-name` | `{"display_name":"New name"}` |
| PATCH | `/api/admin/users/{id}/status` | `{"is_active":false}` или `true` |
| POST | `/api/admin/users/{id}/password-reset` | `{"password":"new-user-supplied-password"}` |
| PUT | `/api/admin/users/{id}/roles/{role_id}` | Назначить роль; идемпотентно |
| DELETE | `/api/admin/users/{id}/roles/{role_id}` | Снять роль; не удаляет target-записи |
| GET | `/api/admin/roles` | `{"roles": [...]}`; роль содержит `id`, `name`, `description`, `is_system`, `permissions` |
| POST | `/api/admin/roles` | Создать пользовательскую роль: `name`, `description`, `permissions`; HTTP 201 |
| PATCH | `/api/admin/roles/{id}` | Изменить `name` и `description` пользовательской роли |
| PUT | `/api/admin/roles/{id}/permissions` | Полностью заменить permissions: `{"permissions":[...]}` |

Успешные mutations, кроме создания, возвращают HTTP 204. Отключение пользователя и сброс пароля атомарно отзывают все его сессии. Отключённый пользователь получает `INVALID_CREDENTIALS` при входе. Запрещены самоотключение и снятие собственной роли `admin`; последнего активного администратора нельзя отключить или лишить admin-прав даже другим администратором. Системные роли `admin` и `viewer` нельзя переименовать или изменить их permissions; API удаления ролей отсутствует.

Права описываются массивом объектов `{"schema":"public","resource":"books","operations":["read","update"]}`. Поддерживаются только `read`, `create`, `update`, `delete`; wildcard разрешён только как `"schema":"*","resource":"*"`. Пустой массив снимает все права пользовательской роли. Пустые имена, частичные wildcards, дубликаты и неизвестные операции отклоняются. Имена пользовательских ролей начинаются со строчной латинской буквы и содержат до 100 символов `a-z`, `0-9`, `_`, `-`; описание — до 500 символов. Email — до 254 символов, display name — до 200, пароль — от 12 до 1024 символов. Все значения передаются в PostgreSQL как параметры, а не интерполируются в SQL.

Изменения ролей и назначений применяются к следующим запросам без перезапуска backend. Права нескольких ролей объединяются. Ошибки валидации используют HTTP 400 `INVALID_ADMIN_REQUEST` и при необходимости `field_errors`. Конфликты включают `ALREADY_EXISTS`, `LAST_ACTIVE_ADMIN`, `SELF_DISABLE_FORBIDDEN`, `SELF_ADMIN_REMOVAL_FORBIDDEN` и `SYSTEM_ROLE_IMMUTABLE`; отсутствующие сущности возвращают `USER_NOT_FOUND` или `ROLE_NOT_FOUND`.

### Field permissions в конфигурации роли

Для concrete schema/resource permission может дополнительно содержать `field_permissions` для `create` и `update`:

```json
{
  "schema": "public",
  "resource": "books",
  "operations": ["read", "create", "update"],
  "field_permissions": [
    {"action": "create", "mode": "unrestricted", "columns": []},
    {"action": "update", "mode": "allowlist", "columns": ["title", "published_on"]}
  ]
}
```

`unrestricted` разрешает все поля, которые иначе доступны действию по metadata. `allowlist` разрешает только явный массив `columns`; `"columns":[]` — самостоятельный режим, при котором ни одно поле не доступно для записи. Отсутствие field configuration в legacy payload означает `unrestricted`; миграция также backfill-ит существующие concrete create/update grants как unrestricted, поэтому прежние возможности ролей сохраняются. В ответах admin/effective permissions concrete create/update grants содержат явную конфигурацию.

Права ролей объединяются по resource/action: один unrestricted даёт effective unrestricted, иначе effective fields — объединение allowlists. Empty allowlist добавляет ноль полей и не отменяет другие роли. Wildcard `*.*` всегда unrestricted, не принимает `field_permissions` и не может быть сужен другой ролью. Системная `admin` unrestricted, `viewer` не получает create/update, системные роли неизменяемы.

API проверяет существование роли, действия create/update, concrete schema/resource, mode, размер и структуру allowlist (до 1000 колонок), duplicates и имена колонок по свежим target metadata. Generated, identity, read-only и PK columns нельзя выбрать как writable. Изменения сохраняются только в admin database `role_field_permissions` в той же транзакции, что и замена resource permissions. DDL/GRANT в target не выполняются; admin session и CSRF обязательны как и прежде.

## GET /api/schema

Возвращает только таблицы и представления, для которых текущий пользователь имеет `read`. Недоступные ресурсы, пустые после фильтрации схемы и внешние ключи на недоступные ресурсы не включаются в ответ. Системные схемы PostgreSQL также исключаются. Без какого-либо права `read` endpoint возвращает HTTP 403 `FORBIDDEN` до introspection.

Успешный ответ (HTTP 200):

```json
{
  "write_enabled": true,
  "schemas": [
    {
      "name": "public",
      "resources": [
        {
          "name": "items",
          "kind": "table",
          "columns": [
            {
              "name": "id",
              "data_type": "bigint",
              "nullable": false,
              "default": "nextval('items_id_seq'::regclass)",
              "primary_key": true,
              "identity": "always",
              "generated": false,
              "editable": false,
              "enum_values": []
            }
          ],
          "primary_key": ["id"],
          "creatable_fields": ["title", "owner_id"],
          "updatable_fields": ["title"],
          "can_create": true,
          "can_update": true,
          "foreign_keys": [
            {
              "name": "items_owner_id_fkey",
              "columns": ["owner_id"],
              "referenced_schema": "public",
              "referenced_table": "owners",
              "referenced_columns": ["id"]
            }
          ]
        }
      ]
    }
  ]
}
```

`kind` принимает значение `table` или `view`. `default` равен `null`, если значение по умолчанию отсутствует. `primary_key` отмечает ключевой столбец. `identity` — пустая строка, `always` или `by_default`; `generated` отмечает сгенерированные столбцы. `editable` истинен только для поддерживаемых несгенерированных столбцов таблицы (`always` identity не редактируется). `enum_values` содержит допустимые метки enum-типов и пуст в остальных случаях. Составные первичные и внешние ключи сохраняют порядок столбцов. Пустые коллекции возвращаются как массивы.

`creatable_fields` и `updatable_fields` — effective field permissions, вычисленные backend из свежих metadata и объединения ролей; frontend не выполняет union самостоятельно. Без соответствующего action permission массив пуст. Unrestricted включает все поля, иначе допустимые действию (для update PK исключаются); allowlist пересекается с этим базовым набором. Empty allowlist возвращает пустой массив и никогда не отображается как unrestricted.

`can_create`/`can_update` учитывают configured mutation pool, action permission и структурную возможность записи. `create_disabled_reason="required_field_unavailable"` означает, что NOT NULL поле без default/generated/identity недоступно и create невозможен. `update_disabled_reason="no_writable_fields"` означает пустой effective update set. При empty create allowlist create всё ещё возможен, если PostgreSQL может создать запись только из default/generated/identity/NULL values. Эти flags не меняют read visibility колонок, resources, history или related labels.

Каждый resource также содержит `reverse_relations` — one-to-many metadata, вычисленную из тех же introspected FK без дополнительных объектов в target schema. Элемент содержит constraint `name`, дочерние `schema`/`resource`, дочерние local `columns` и parent `referenced_columns` в соответствующем порядке. Например, для `authors` связь имеет `{"name":"books_author_id_fkey","schema":"public","resource":"books","columns":["author_id"],"referenced_columns":["id"]}`. Reverse relations создаются только после RBAC-фильтрации: ресурсы без `read`, их FK и имена недоступных child resources не включаются и не раскрываются.

`write_enabled` истинен только когда настроен mutation pool из единого `TARGET_DATABASE_URL` и хотя бы один видимый resource имеет реально доступные create/update либо structurally поддерживаемый delete. Он не гарантирует запись в каждый ресурс или столбец и не заменяет PostgreSQL privileges. Read pool остаётся read-only; отдельного writer DSN нет.

Если target database не настроена, endpoint возвращает HTTP 503:

```json
{
  "error": {
    "code": "TARGET_NOT_CONFIGURED",
    "message": "Target database is not configured"
  }
}
```

Другие ошибки интроспекции возвращают HTTP 500 с кодом `SCHEMA_INTROSPECTION_FAILED`.

## GET /api/resources/{schema}/{resource}/records

Возвращает одну страницу записей таблицы или представления. `schema` и `resource` должны точно совпадать с метаданными из `GET /api/schema`; поддерживается URL-кодирование пути. Каждый запрос использует свежие метаданные интроспекции. Неизвестные схемы, ресурсы и столбцы отклоняются до выполнения SQL.

Endpoint доступен только для чтения и требует `read` для указанного ресурса; иначе возвращается HTTP 403 `FORBIDDEN` до introspection. Динамические идентификаторы экранируются после проверки по метаданным, а все значения поиска и фильтров передаются как параметры запроса PostgreSQL. SQL-выражения в `sort`, `direction`, `search`, `search_column` и `filter` не поддерживаются.

### Параметры запроса

- `page`: положительное целое число, по умолчанию `1`.
- `limit`: целое число от `1` до `100`, по умолчанию `50`.
- `sort`: одно разрешённое имя столбца. Выражения и сортировка по нескольким столбцам через запятую не принимаются.
- `direction`: `asc` или `desc`, по умолчанию `asc`. Дополнительные ключи сортировки по первичному ключу используют то же направление.
- `search`: буквальная подстрока без учёта регистра, поиск выполняется через `ILIKE`. Символы `%`, `_` и `\` экранируются и не имеют смысла wildcards.
- `search_column`: повторяемое имя текстового столбца. По умолчанию `search` охватывает все поддерживаемые текстовые столбцы ресурса. Нетекстовые и неизвестные столбцы отклоняются.
- `filter`: URL-кодированный JSON-массив не более чем с 20 объектами. Кодированное значение не должно превышать 64 КиБ. Каждый объект содержит `column`, `operator` и `value`:

```json
[
  {"column": "title", "operator": "contains", "value": "postgres"},
  {"column": "id", "operator": "gte", "value": "100"},
  {"column": "published_on", "operator": "is_null", "value": false}
]
```

Значения сравнения — JSON-строки даже для booleans и numbers, поэтому большие целые и десятичные числа не проходят через JavaScript numbers. `is_null` принимает JSON boolean: `true` означает `IS NULL`, `false` — `IS NOT NULL`. Несколько фильтров объединяются через `AND`.

Поддерживаемые фильтры по типам PostgreSQL:

- `text`, `character varying`, `character`, `citext`, `name`: `eq`, `neq`, `contains`, `is_null`.
- `boolean`: `eq`, `neq`, `is_null`; значения — `true` или `false`.
- `smallint`, `integer`, `bigint`, `real`, `double precision`, `numeric`, `decimal`: `eq`, `neq`, `lt`, `lte`, `gt`, `gte`, `is_null`.
- `date`: те же операторы сравнения; значения в формате `YYYY-MM-DD`.
- `timestamp without time zone`: те же операторы сравнения; значения в формате `YYYY-MM-DDTHH:MM:SS` с необязательными долями секунды.
- `timestamp with time zone`: те же операторы сравнения; значения в формате RFC 3339.
- `uuid`: те же операторы сравнения; значения в каноническом формате UUID.
- `json`, `jsonb`, `bytea` и другие типы: только `is_null`.

Сортировка доступна для text, boolean, integer, floating-point, numeric, date, timestamp, UUID, `bytea` и `jsonb`. Другие типы, включая `json`, не поддерживают сортировку через этот endpoint.

### Успешный ответ

HTTP 200:

```json
{
  "records": [
    {
      "id": "9223372036854775807",
      "price": "12345678901234567890.01",
      "active": true,
      "published_on": "2025-01-02",
      "created_at": "2025-01-02T03:04:05.123456789Z",
      "external_id": "00112233-4455-6677-8899-aabbccddeeff",
      "metadata": {"edition": 1, "big": 9223372036854775807},
      "payload": "AAH/",
      "description": null
    }
  ],
  "page": 1,
  "limit": 50,
  "has_next": false
}
```

Ключи записей — имена столбцов из `/api/schema`. Представления в JSON:

- SQL `NULL` → JSON `null`.
- `boolean` → JSON boolean.
- `smallint`, `integer`, `real`, `double precision` → JSON number; нефинитные значения с плавающей точкой (`NaN`, бесконечности) → JSON string.
- `bigint`, `numeric`, `decimal` → JSON string с сохранением целой и десятичной точности.
- `date` → строка `YYYY-MM-DD`.
- timestamps → строки ISO 8601/RFC 3339 с долями секунды, если они присутствуют.
- `uuid` → каноническая строка UUID.
- `json`, `jsonb` → исходное JSON-значение; JSON numbers не декодируются через JavaScript `number`.
- `bytea` → стандартная строка Base64.
- Другие типы PostgreSQL → безопасное строковое представление.

### Пагинация

Запрос получает `limit + 1` строк для вычисления `has_next`; `COUNT(*)` и общее число записей не возвращаются. Смещение равно `(page - 1) * limit`. Большие смещения могут быть ресурсоёмкими.

При наличии первичного ключа его столбцы добавляются к запрошенной сортировке как уникальные tie-breakers. Без явной сортировки по умолчанию используется первичный ключ. Для ресурсов без первичного ключа стабильный порядок между страницами не гарантируется, особенно при изменении исходных данных.

### Ошибки

Ошибки используют ту же обёртку `error.code` и `error.message`, что и интроспекция схемы:

- HTTP 400: `INVALID_PAGE`, `INVALID_LIMIT`, `INVALID_SORT`, `INVALID_DIRECTION`, `INVALID_SEARCH`, `INVALID_FILTER` или `INVALID_QUERY`.
- HTTP 404: `RESOURCE_NOT_FOUND`, если схема или ресурс отсутствуют в метаданных интроспекции.
- HTTP 503: `TARGET_NOT_CONFIGURED`.
- HTTP 500: `SCHEMA_INTROSPECTION_FAILED` или `RECORDS_QUERY_FAILED`.

Запросы записей и интроспекции имеют backend timeout 10 секунд и используют контекст запроса, поэтому отмена клиентом передаётся в PostgreSQL.

## POST /api/resources/{schema}/{resource}/relations/resolve

Read-only batch endpoint для related labels в списке записей. Несмотря на POST, он не изменяет состояние: CSRF не требуется, writer DSN и audit не используются. Требуются session, `read` на текущий resource из пути и `read` на каждый referenced resource. Отсутствие session возвращает 401, отсутствие любого права — 403 `FORBIDDEN` без login redirect и без раскрытия имени недоступного ресурса.

Тело запроса содержит known FK relations и ключи-объекты ровно с referenced columns:

```json
{
  "relations": [
    {
      "name": "books_author_id_fkey",
      "keys": [{"id":"9223372036854775807"}, {"id":null}]
    },
    {
      "name": "children_parent_fk",
      "keys": [{"tenant_id":"7", "external_id":"00112233-4455-6677-8899-aabbccddeeff"}]
    }
  ]
}
```

Поддерживаются simple/composite keys. Bigint/numeric передаются строками, UUID — canonical string; integer JSON numbers также принимаются. Ключи дедуплицируются внутри relation, а ключ с `null` хотя бы в одной части пропускается. Пустой `keys` или только NULL-ключи возвращают пустой `records` без SQL-запроса. Лимиты: тело до 256 КиБ, не более 20 relations, 200 входных keys на relation и 1000 входных keys суммарно. Duplicate relation names, unknown relation/column/resource, лишние или отсутствующие части ключа и неверные типы отклоняются с HTTP 400 `INVALID_RELATION_RESOLVE`.

Ответ сохраняет порядок relations:

```json
{
  "relations": [
    {
      "name": "books_author_id_fkey",
      "records": [{"id":"9223372036854775807", "name":"Author"}]
    }
  ]
}
```

Для каждой relation выполняется один parameterized SELECT через read-only target pool, независимо от числа ключей. Schema, resource, relation и columns проверяются по свежему metadata allowlist; identifiers quoting, произвольный SQL не принимается. Выбираются только referenced key, PK и текстовые поля, необходимые общему display label helper. Формат значений совпадает с records API, включая точные bigint/numeric, UUID, JSON/JSONB и Base64 bytea для выбранных ключевых полей. Ошибка запроса возвращает HTTP 500 `RELATION_RESOLVE_FAILED` без SQL или stack traces.

## Many-to-one selectors в формах записей

Frontend использует существующие `foreign_keys` из `/api/schema` без отдельной конфигурации ресурсов. Для каждого many-to-one FK учитываются упорядоченные `columns`, `referenced_schema`, `referenced_table`, `referenced_columns`, `nullable`, `editable` и effective `creatable_fields`/`updatable_fields` локальных столбцов. Simple FK заменяется одним selector; все части composite FK объединяются в один логический selector. Если столбец нельзя редактировать или это PK при update, сохраняется read-only/raw control. Если FK недоступен для текущего действия field permission, selector и unauthorized editable controls скрываются, а старые значения не включаются в payload; read-only relation panels и labels не меняются.

Selector запрашивает только первую страницу через существующий records list endpoint (`limit=20`), выполняет server-side search с debounce 300 ms и добавляет следующие страницы кнопкой **Load more**. Вся referenced table не загружается. При editing текущее значение отдельно запрашивается точными `eq` filters по всем referenced columns с `limit=1`, поэтому оно отображается, даже если отсутствует на первой странице. До загрузки label или при ошибке технический key не заменяется пустым значением.

Человекочитаемый label выбирается generic-образом в порядке `display_name`, `name`, `title`, `label`, другое непустое текстовое поле, затем technical identifier. Рядом выводятся все referenced key columns в порядке FK, чтобы различать одинаковые имена и составные ключи. Используются только поля, возвращённые authorized records API.

Selector заполняет только существующие локальные FK columns в прежнем flat `values` для POST/PATCH. Composite FK отправляется либо целиком, либо, если все его части nullable, очищается целиком в JSON `null`; частичное значение блокируется клиентской validation. Required relation нельзя очистить. Bigint/numeric и UUID остаются строками, а `null` не преобразуется в строку. Nested payloads, автоматическое создание или редактирование связанных записей и новые mutation endpoints отсутствуют. CSRF, server validation, constraint errors, audit и запрет automatic retry после `AUDIT_WRITE_FAILED` не меняются.

Referenced records запрашиваются только при наличии `read` на referenced resource и его metadata в авторизованном `/api/schema`. Иначе related requests и labels отсутствуют; остаётся существующий raw FK input, а если FK metadata известна, но referenced resource недоступен, показывается локализованное пояснение без technical internals. Backend по-прежнему полностью исключает FK на недоступный ресурс из `/api/schema`, поэтому такая фильтрация сама по себе не раскрывает referenced resource. Read-only controls не допускают изменения selector; `create`, `update` и `delete` остаются отдельными backend permissions.

## Related labels и read-only one-to-many panels

В `RecordsTable` many-to-one FK cells используют тот же общий display label helper, что и selectors. Один batch resolve на группу relations заменяет per-row/per-key HTTP-запросы; повторяющиеся FK values дедуплицируются. Рядом или в accessible tooltip остаются все referenced key parts, поэтому одинаковые labels различаются по ключу. NULL показывается локализованным empty relation, длинные labels обрезаются визуально с полным `title`. При ошибке resolution основной список и raw FK values остаются доступными. Без `read` на referenced resource frontend не отправляет related requests и не получает labels/metadata через schema.

На parent record form/detail `reverse_relations` порождают generic read-only child panels. Detail доступен по `read` даже без `update`; в нём нет editable или save controls. Каждая панель использует существующий records list endpoint с typed `eq` filters по всем child local columns, включая каждую часть composite relation. Если тип parent key не поддерживает существующий `eq` filter, такая reverse panel на этом этапе скрывается и запрос не выполняется. Отдельная pagination использует тот же `page`/`limit`/`has_next` contract. Child key и основные поля форматируются существующими safe helpers; loading, empty, 401, 403 и error states изолированы внутри конкретной панели и не затрагивают parent form или другие панели. Для несохранённой parent record показывается **Save the record first**, child query без полного parent key не выполняется. Переключение ru/en не меняет pagination и значения формы.

Матрица прав: `read` только на parent открывает parent и скрывает child panels/related labels; `read` на parent и child разрешает child records и labels; `read` только на referenced resource не открывает current relation resolve. Backend независимо проверяет те же права для schema, resolve и records list. Все операции используют read pool, audit events не создаются. В one-to-many panels нет create/edit/delete controls и nested record forms. Nested mutations и создание дочерних записей из parent form не реализованы.

## Many-to-many editor через junction tables

Frontend generic-образом определяет many-to-many relations только по RBAC-filtered metadata `/api/schema`, без имён конкретных ресурсов и без новых metadata или mutation endpoints. Junction table поддерживается, если это таблица ровно с двумя many-to-one FK к двум разным parent/target resources, её composite PK состоит ровно из всех колонок обоих FK в любом порядке, а каждая дополнительная колонка nullable либо имеет default/generated/identity value. Все FK columns должны поддерживать обычный POST, referenced key columns — существующий `eq` filter. Self-referential many-to-many, отдельный surrogate PK, три и более FK и дополнительные обязательные attribute fields без default/generated не поддерживаются: такие ресурсы и их reverse panels остаются доступны обычным образом, но editor для них не показывается.

Для панели необходимы `read` на parent, target и junction. Недоступные ресурсы не раскрываются и related requests не выполняются. Панель запрашивает страницу junction records через `GET /api/resources/{schema}/{junction}/records` с typed `eq` filters по всем parent FK columns и получает target labels одним batch `POST /api/resources/{schema}/{junction}/relations/resolve` на страницу, а не запросом на строку. Поддерживаются simple/composite parent, target и junction keys, точные bigint/numeric строки, UUID и независимая pagination. Поддерживаемая junction reverse panel не дублируется вместе с many-to-many panel.

Для **Add relation** дополнительно нужны настроенный writer, `create` на junction, все необходимые junction FK columns в effective `creatable_fields` и `read` на target для selector. При empty allowlist или отсутствии хотя бы одной FK column Add relation скрывается; read-only связи и независимый delete behavior не меняются. Используется обычный `POST /api/resources/{schema}/{junction}/records` с flat `values`, содержащими только колонки двух FK; parent/target records не изменяются. Selector использует существующий records list с server-side search, debounce и Load more, без создания target records. `UNIQUE_VIOLATION` означает duplicate relation; failed POST не создаёт audit event и не повторяется автоматически.

Для **Remove relation** дополнительно нужны настроенный writer и отдельное `delete` на junction. Используется существующий `DELETE /api/resources/{schema}/{junction}/records/{key}` с URL-кодированным полным composite junction PK. Confirmation удаляет только junction record и явно отличается от Delete record: parent и target records не удаляются и не обновляются. После 204 или committed audit failure список обновляется без automatic retry; при удалении последней связи страницы выполняется переход назад.

Create/delete связей аудируются как обычные `create`/`delete` события junction resource с actor, полным record key, фактическим after для create и before/`after=null` для delete. Дополнительные события для parent/target не создаются. При `AUDIT_WRITE_FAILED` после target commit сохраняется общее поведение `mutation_committed=true`: связь уже добавлена/удалена, компенсации и automatic retry отсутствуют, UI обновляется с отдельным предупреждением. Nested mutations, создание связанных записей и редактирование дополнительных junction attributes не реализованы; такие attributes можно редактировать только через обычную форму junction resource, если структура и права это позволяют.

## POST /api/resources/{schema}/{resource}/records

Создаёт запись в таблице. Требуются право `create` для ресурса и действительный CSRF token; без права возвращается HTTP 403 `FORBIDDEN`. Чтение метаданных и mutation используют отдельные пулы, созданные из одного `TARGET_DATABASE_URL`. Роль PostgreSQL должна получить необходимые права от владельца target database.

Тело запроса:

```json
{
  "values": {
    "title": "PostgreSQL Guide",
    "author_id": "1",
    "price": "12345678901234567890.01",
    "active": true,
    "published_on": "2025-01-02",
    "metadata": {"edition": 1}
  }
}
```

Вставляются только поля, присутствующие в `values`. Пропущенные generated- и identity-столбцы, а также столбцы со значениями по умолчанию остаются на усмотрение PostgreSQL. Клиент может получить поведение `DEFAULT VALUES`, отправив пустой объект `values`, если все обязательные поля имеют значения по умолчанию или допускают NULL. JSON `null` означает SQL `NULL` и отклоняется для non-nullable столбцов. Пустые текстовые строки остаются пустыми строками и не преобразуются в `NULL`.

После resource-level `create` check backend проверяет каждое переданное поле по effective `creatable_fields`, независимо от frontend. Поле вне effective set возвращает HTTP 403 `FIELD_FORBIDDEN`; смешанный payload отклоняется целиком, INSERT и audit не выполняются. Если обязательное NOT NULL поле без default/generated/identity недоступно, возвращается structured HTTP 403 `CREATE_IMPOSSIBLE` до неизбежной database error, без SQL/stack traces. Defaults/generated/identity сохраняют прежнее поведение.

Значения `bigint`, `numeric` и `decimal` следует передавать как JSON-строки для сохранения точности. Целые значения могут быть JSON numbers или strings. Даты, timestamps и UUID используют те же форматы, что и фильтры записей. JSON и JSONB принимают любое допустимое JSON-значение. Значения enum должны соответствовать `enum_values`. Неподдерживаемые типы PostgreSQL, generated-столбцы и `always` identity не могут быть переданы.

Успешный ответ — HTTP 201. PostgreSQL `RETURNING` возвращает полную сохранённую запись в том же безопасном JSON-представлении, что и endpoint списка:

```json
{
  "record": {
    "id": "9223372036854775807",
    "title": "PostgreSQL Guide"
  }
}
```

## PATCH /api/resources/{schema}/{resource}/records/{key}

Обновляет только переданные редактируемые поля, не входящие в первичный ключ, и возвращает полную обновлённую запись с HTTP 200. Требуются право `update` и действительный CSRF token; без права возвращается HTTP 403 `FORBIDDEN`. Представления и таблицы без первичного ключа доступны только для чтения. Обновление первичных ключей не поддерживается.

Все переданные поля должны входить в effective `updatable_fields`. Запрещённое поле возвращает HTTP 403 `FIELD_FORBIDDEN` и отклоняет весь PATCH: разрешённые поля смешанного запроса не изменяются, UPDATE и audit не выполняются. Empty effective set возвращает `NO_WRITABLE_FIELDS`. PK/generated/read-only/unknown columns сохраняют прежнюю metadata validation и не обходятся через field permissions.

`{key}` — **URL-кодированный JSON-объект**, содержащий ровно те столбцы, которые входят в первичный ключ ресурса. Порядок полей объекта не важен. Значения используют JSON-представление запроса, включая строки для bigint- и numeric-ключей. Примеры до URL-кодирования:

```json
{"id":"42"}
```

```json
{"book_id":"10","category_id":"20"}
```

Например, `{"id":"42"}` кодируется как `%7B%22id%22%3A%2242%22%7D`. Отсутствующие, null, дополнительные или неизвестные поля ключа возвращают `INVALID_RECORD_KEY`.

Тела запроса и успешного ответа имеют тот же формат, что и при создании. Пустой объект `values` отклоняется. Обновления используют семантику **last write wins**: в этой итерации нет столбца версии, optimistic lock или обнаружения конкурентных изменений.

## DELETE /api/resources/{schema}/{resource}/records/{key}

Удаляет одну запись таблицы и возвращает HTTP 204 без тела. Требуются session, отдельное право `delete` для ресурса и действительный CSRF token. Mutation pool создаётся из того же `TARGET_DATABASE_URL`; read pool остаётся read-only и никогда не используется для удаления.

`{key}` имеет тот же URL-кодированный JSON-формат, что и PATCH, и поддерживает simple/composite PK. Schema, resource и PK проверяются по свежему metadata allowlist. Идентификаторы quoting, значения передаются только как SQL parameters. Представления и таблицы без PK возвращают `RESOURCE_READ_ONLY`. Тело запроса не используется. Bulk delete и удаление нескольких записей одним запросом не поддерживаются.

Backend выполняет `DELETE … RETURNING` и получает фактические before-значения от target database, а не от клиента. Если запись отсутствует, возвращается 404 `RECORD_NOT_FOUND` без audit. Любой failed DELETE, включая FK/constraint error, также не создаёт audit. Constraint responses не содержат SQL, stack traces или внутренних сведений.

После target commit событие `delete` записывается в admin database с фактическим `before_data`, `after_data IS NULL`, actor из серверной сессии и simple/composite record key. Межбазовой атомарности нет. Если DELETE committed, но audit failed, возвращается HTTP 500 `AUDIT_WRITE_FAILED` с `mutation_committed=true`; запись уже удалена. Backend не повторяет DELETE и не выполняет компенсирующий INSERT. Frontend показывает отдельное локализованное предупреждение и обновляет список без повторного DELETE.

### Ошибки изменения записей

Обёртка ошибок совпадает с endpoints чтения. Ошибки валидации и некоторые ошибки ограничений дополнительно содержат `field_errors` с ключами по именам столбцов:

```json
{
  "error": {
    "code": "INVALID_RECORD",
    "message": "Record validation failed",
    "field_errors": {"published_on": "Expected a date in YYYY-MM-DD format"}
  }
}
```

- HTTP 400: `INVALID_RECORD`, `INVALID_RECORD_KEY`, `NOT_NULL_VIOLATION`, `FOREIGN_KEY_VIOLATION` или `CHECK_VIOLATION`.
- HTTP 403: `FORBIDDEN`, если отсутствует требуемое resource право; `FIELD_FORBIDDEN` для поля вне effective set; `NO_WRITABLE_FIELDS` при пустом update set; `CREATE_IMPOSSIBLE`, если required field недоступен для create; `CSRF_TOKEN_INVALID`, если CSRF token отсутствует или неверен. Field-forbidden requests не создают mutation/audit. Read pool остаётся read-only, а database privileges дополнительно ограничивают mutations.
- HTTP 404: `RESOURCE_NOT_FOUND` или `RECORD_NOT_FOUND`.
- HTTP 409: `RESOURCE_READ_ONLY` или `UNIQUE_VIOLATION`.
- HTTP 503: `TARGET_NOT_CONFIGURED`.
- HTTP 500: `SCHEMA_INTROSPECTION_FAILED`, `RECORD_MUTATION_FAILED` или `AUDIT_WRITE_FAILED` с `mutation_committed=true`, если target mutation уже committed, но audit не записан.

Ответы об ошибках ограничений используют общие сообщения и не раскрывают SQL, stack traces или внутренние сведения сервера. Размер POST/PATCH-запросов ограничен 1 МиБ, backend timeout составляет 10 секунд. DELETE не принимает тело и удаляет только одну запись.

После успешного POST, PATCH или DELETE backend записывает событие `create`, `update` или `delete` в `audit_events` **в admin database**. Actor определяется по серверной сессии, timestamp и UUID генерируются сервером. Для create сохраняется фактический `after` из PostgreSQL `RETURNING`; для update — фактические `before` и `after`; для delete — фактический `before` из `DELETE … RETURNING` и `after = null`. Перед UPDATE запись блокируется и читается в той же target-транзакции, поэтому before не принимается от клиента. Поддерживаются простые и составные первичные ключи.

Target mutation и audit находятся в разных базах и **не образуют атомарную транзакцию**. Сначала завершается target mutation, затем создаётся audit event. Ошибка target mutation не создаёт событие. Если target уже committed, но запись audit не удалась, возвращается HTTP 500 с отдельной ошибкой; для POST/PATCH ответ включает сохранённую запись:

```json
{
  "error": {
    "code": "AUDIT_WRITE_FAILED",
    "message": "The target record was saved, but its audit event could not be recorded",
    "mutation_committed": true
  },
  "record": {"id": "42", "title": "Saved in target"}
}
```

`mutation_committed=true` означает, что target mutation уже committed и **не был откачен**. Backend не выполняет компенсирующие DELETE/UPDATE/INSERT, двухфазный commit или автоматический повтор POST/PATCH/DELETE. Клиент не должен автоматически повторять mutation; следует обновить список и проверить состояние. Для DELETE запись уже удалена, поэтому ответ не содержит `record`, а frontend показывает отдельное предупреждение об удалении без гарантированного audit.

## История изменений через Velmir

Таблица `audit_events` существует только в admin database и содержит:

- `id UUID PRIMARY KEY` — уникальный ID, по умолчанию `gen_random_uuid()`;
- `occurred_at TIMESTAMPTZ` — серверное время, по умолчанию `clock_timestamp()`;
- `actor_user_id BIGINT` — ссылка на `users(id)` фактического аутентифицированного actor;
- `action TEXT` — `create`, `update` или `delete`;
- `schema_name`, `resource_name` — технические идентификаторы ресурса;
- `record_key JSONB` — объект с простым или составным первичным ключом;
- `before_data JSONB` — `NULL` для create, фактическое состояние для update/delete;
- `after_data JSONB` — фактическое состояние для create/update и `NULL` для delete.

Check constraint требует after для create, before/after для update и только before для delete. Индексы ограничивают запрос конкретным ресурсом и ключом; endpoint для неограниченного чтения всей таблицы отсутствует. Миграции Goose транзакционны и используют идемпотентные DDL-операции. Velmir не создаёт audit-таблицы, triggers или functions в target database.

Поля, по имени содержащие пароли, session/CSRF tokens, password hashes, secrets или connection strings, исключаются из audit, включая вложенные JSON-объекты. Ошибки логируются только с типом ошибки и без connection strings, значений записей и секретов. Bigint/numeric используют строки records API, bytea — Base64, а JSON/JSONB обрабатывается как `json.RawMessage`/`json.Number` без преобразования через JavaScript number.

История охватывает только успешные изменения, выполненные через Velmir после внедрения этой функции. Она **не гарантирует фиксацию внешних изменений**, сделанных напрямую в target database. Login/logout, чтение и администрирование пользователей не записываются в audit. Forbidden field mutations не аудируются; успешные create/update сохраняют прежние actor, record key и before/after, а `AUDIT_WRITE_FAILED` сохраняет `mutation_committed=true` без automatic retry. Field-level read filtering не добавляется: history использует resource-level `read`. Для таблицы без первичного ключа create может сохранить событие с пустым ключом, но history конкретной записи недоступна.

## GET /api/resources/{schema}/{resource}/records/{key}/history

Возвращает историю конкретной записи в обратном хронологическом порядке (`occurred_at DESC`, затем `id DESC`). Требуются session и право `read` на ресурс: без session возвращается 401 `UNAUTHENTICATED`, без read — 403 `FORBIDDEN` до introspection и чтения audit. Недоступные ресурсы и их история не раскрываются. CSRF для GET не требуется.

`{key}` использует тот же URL-кодированный JSON-объект, что и PATCH, включая составные ключи. Schema, resource и ключевые столбцы проверяются по свежим metadata allowlist. Параметры запроса:

- `page` — положительное целое, по умолчанию `1`; переполнение offset отклоняется;
- `limit` — от `1` до `100`, по умолчанию `20`.

Ответ содержит не более `limit` событий и `has_next`, вычисленное через запрос `limit + 1`; общее число событий и неограниченная выгрузка не предоставляются. Отсутствие истории — HTTP 200 с пустым массивом:

```json
{
  "events": [
    {
      "id": "00112233-4455-6677-8899-aabbccddeeff",
      "actor": {"id": 1, "email": "admin@example.com", "display_name": "Administrator"},
      "timestamp": "2025-01-02T03:04:05.123456Z",
      "action": "update",
      "schema": "public",
      "resource": "items",
      "record_key": {"tenant_id": "7", "id": "42"},
      "before": {"title": "Before", "price": "1.00"},
      "after": {"title": "After", "price": "12345678901234567890.01"}
    }
  ],
  "page": 1,
  "limit": 20,
  "has_next": false
}
```

`before` равен `null` для create, а `after` равен `null` для delete. История удалённой записи возвращается при повторном запросе с её key; существование target-записи не требуется и не проверяется. Record key и before/after используют то же безопасное JSON representation, что и records API: точные bigint/numeric строки, JSON/JSONB без number-конвертации на backend, Base64 bytea, строки дат/timestamps/UUID. Ошибки: 400 `INVALID_RECORD_KEY`, `INVALID_PAGE`, `INVALID_LIMIT`; 401/403 как указано выше; 404 `RESOURCE_NOT_FOUND`; 500 `SCHEMA_INTROSPECTION_FAILED`, `HISTORY_QUERY_FAILED`; 503 `TARGET_NOT_CONFIGURED` или `AUDIT_UNAVAILABLE`.

## GET /readyz

Возвращает состояния `admin_database` и `target_database`. Каждое состояние — `ready`, `unavailable` или `not_configured`. Корректное JSON-тело описывает состояния компонентов даже при HTTP 503.

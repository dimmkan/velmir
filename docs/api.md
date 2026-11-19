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

Доступ по умолчанию запрещён и проверяется на backend независимо от frontend. Пользователю можно назначить несколько ролей; их права объединяются. Системная роль `admin` даёт `read`, `create` и `update` для всех ресурсов, а `viewer` — только `read`. Resource-level grant задаётся схемой, ресурсом и операцией; `*` в системных ролях означает все схемы и ресурсы. Неизвестные роли и отсутствие ролей не дают дополнительных прав.

Аутентифицированный пользователь без необходимого права получает HTTP 403 с кодом `FORBIDDEN`; это отличается от HTTP 401 при отсутствии сессии и от `CSRF_TOKEN_INVALID`. `GET /api/auth/me` остаётся доступным и возвращает пустой список permissions пользователю без роли. Публичная регистрация и DELETE target-записей недоступны. Управление пользователями и ролями разрешено только системной роли `admin`.

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
      "operations": ["read", "create", "update"]
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

Права описываются массивом объектов `{"schema":"public","resource":"books","operations":["read","update"]}`. Поддерживаются только `read`, `create`, `update`; wildcard разрешён только как `"schema":"*","resource":"*"`. Пустой массив снимает все права пользовательской роли. Пустые имена, частичные wildcards, дубликаты и неизвестные операции отклоняются. Имена пользовательских ролей начинаются со строчной латинской буквы и содержат до 100 символов `a-z`, `0-9`, `_`, `-`; описание — до 500 символов. Email — до 254 символов, display name — до 200, пароль — от 12 до 1024 символов. Все значения передаются в PostgreSQL как параметры, а не интерполируются в SQL.

Изменения ролей и назначений применяются к следующим запросам без перезапуска backend. Права нескольких ролей объединяются. Ошибки валидации используют HTTP 400 `INVALID_ADMIN_REQUEST` и при необходимости `field_errors`. Конфликты включают `ALREADY_EXISTS`, `LAST_ACTIVE_ADMIN`, `SELF_DISABLE_FORBIDDEN`, `SELF_ADMIN_REMOVAL_FORBIDDEN` и `SYSTEM_ROLE_IMMUTABLE`; отсутствующие сущности возвращают `USER_NOT_FOUND` или `ROLE_NOT_FOUND`.

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

`write_enabled` истинен только когда настроен необязательный write pool и текущий пользователь имеет хотя бы одно право `create` или `update`. Флаг не гарантирует возможность записи в каждый ресурс или столбец; для controls frontend дополнительно использует effective permissions.

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

## POST /api/resources/{schema}/{resource}/records

Создаёт запись в таблице. Требуются право `create` для ресурса и действительный CSRF token; без права возвращается HTTP 403 `FORBIDDEN`. Endpoint доступен только при настроенном `TARGET_WRITE_DATABASE_URL`; интроспекция и проверка идентификаторов по-прежнему используют свежие метаданные, прочитанные через `TARGET_DATABASE_URL`.

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

`{key}` — **URL-кодированный JSON-объект**, содержащий ровно те столбцы, которые входят в первичный ключ ресурса. Порядок полей объекта не важен. Значения используют JSON-представление запроса, включая строки для bigint- и numeric-ключей. Примеры до URL-кодирования:

```json
{"id":"42"}
```

```json
{"book_id":"10","category_id":"20"}
```

Например, `{"id":"42"}` кодируется как `%7B%22id%22%3A%2242%22%7D`. Отсутствующие, null, дополнительные или неизвестные поля ключа возвращают `INVALID_RECORD_KEY`.

Тела запроса и успешного ответа имеют тот же формат, что и при создании. Пустой объект `values` отклоняется. Обновления используют семантику **last write wins**: в этой итерации нет столбца версии, optimistic lock или обнаружения конкурентных изменений.

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
- HTTP 403: `FORBIDDEN`, если отсутствует требуемое право; `CSRF_TOKEN_INVALID`, если CSRF token отсутствует или неверен; `TARGET_READ_ONLY`, если `TARGET_WRITE_DATABASE_URL` отсутствует. Read pool остаётся read-only.
- HTTP 404: `RESOURCE_NOT_FOUND` или `RECORD_NOT_FOUND`.
- HTTP 409: `RESOURCE_READ_ONLY` или `UNIQUE_VIOLATION`.
- HTTP 503: `TARGET_NOT_CONFIGURED`.
- HTTP 500: `SCHEMA_INTROSPECTION_FAILED` или `RECORD_MUTATION_FAILED`.

Ответы об ошибках ограничений используют общие сообщения и не раскрывают SQL, stack traces или внутренние сведения сервера. Размер mutation-запросов ограничен 1 МиБ, backend timeout составляет 10 секунд. DELETE endpoints не реализованы.

## GET /readyz

Возвращает состояния `admin_database` и `target_database`. Каждое состояние — `ready`, `unavailable` или `not_configured`. Корректное JSON-тело описывает состояния компонентов даже при HTTP 503.

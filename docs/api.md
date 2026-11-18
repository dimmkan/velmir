# API

## GET /api/schema

Returns user schemas, tables and views from the configured target database. System schemas are excluded. The endpoint reads PostgreSQL metadata only.

Successful response (HTTP 200):

```json
{
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
              "default": "nextval('items_id_seq'::regclass)"
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

`kind` is `table` or `view`. `default` is `null` when no default exists. Composite primary and foreign keys preserve column order. Empty collections are returned as arrays.

If no target database is configured, the endpoint returns HTTP 503:

```json
{
  "error": {
    "code": "TARGET_NOT_CONFIGURED",
    "message": "Target database is not configured"
  }
}
```

Other introspection failures return HTTP 500 with code `SCHEMA_INTROSPECTION_FAILED`.

## GET /readyz

Returns `admin_database` and `target_database` states. Each state is `ready`, `unavailable` or `not_configured`. A valid JSON body describes component states even when the HTTP status is 503.

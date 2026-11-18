package introspection

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type databaseQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type Inspector struct {
	database databaseQuerier
}

func New(database databaseQuerier) *Inspector {
	return &Inspector{database: database}
}

func (i *Inspector) Inspect(ctx context.Context) (Catalog, error) {
	objects, err := i.queryObjects(ctx)
	if err != nil {
		return Catalog{}, err
	}
	columns, err := i.queryColumns(ctx)
	if err != nil {
		return Catalog{}, err
	}
	primaryKeys, err := i.queryPrimaryKeys(ctx)
	if err != nil {
		return Catalog{}, err
	}
	foreignKeys, err := i.queryForeignKeys(ctx)
	if err != nil {
		return Catalog{}, err
	}
	return buildCatalog(objects, columns, primaryKeys, foreignKeys), nil
}

func (i *Inspector) queryObjects(ctx context.Context) ([]objectRow, error) {
	rows, err := i.database.Query(ctx, objectsQuery)
	if err != nil {
		return nil, fmt.Errorf("query objects: %w", err)
	}
	defer rows.Close()

	objects := make([]objectRow, 0)
	for rows.Next() {
		var object objectRow
		if err := rows.Scan(&object.Schema, &object.Name, &object.Kind); err != nil {
			return nil, fmt.Errorf("scan object: %w", err)
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read objects: %w", err)
	}
	return objects, nil
}

func (i *Inspector) queryColumns(ctx context.Context) ([]columnRow, error) {
	rows, err := i.database.Query(ctx, columnsQuery)
	if err != nil {
		return nil, fmt.Errorf("query columns: %w", err)
	}
	defer rows.Close()

	columns := make([]columnRow, 0)
	for rows.Next() {
		var column columnRow
		if err := rows.Scan(
			&column.Schema,
			&column.Resource,
			&column.Name,
			&column.DataType,
			&column.Nullable,
			&column.Default,
			&column.Identity,
			&column.Generated,
			&column.EnumValues,
		); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read columns: %w", err)
	}
	return columns, nil
}

func (i *Inspector) queryPrimaryKeys(ctx context.Context) ([]primaryKeyRow, error) {
	rows, err := i.database.Query(ctx, primaryKeysQuery)
	if err != nil {
		return nil, fmt.Errorf("query primary keys: %w", err)
	}
	defer rows.Close()

	primaryKeys := make([]primaryKeyRow, 0)
	for rows.Next() {
		var primaryKey primaryKeyRow
		if err := rows.Scan(&primaryKey.Schema, &primaryKey.Resource, &primaryKey.Column); err != nil {
			return nil, fmt.Errorf("scan primary key: %w", err)
		}
		primaryKeys = append(primaryKeys, primaryKey)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read primary keys: %w", err)
	}
	return primaryKeys, nil
}

func (i *Inspector) queryForeignKeys(ctx context.Context) ([]foreignKeyRow, error) {
	rows, err := i.database.Query(ctx, foreignKeysQuery)
	if err != nil {
		return nil, fmt.Errorf("query foreign keys: %w", err)
	}
	defer rows.Close()

	foreignKeys := make([]foreignKeyRow, 0)
	for rows.Next() {
		var foreignKey foreignKeyRow
		if err := rows.Scan(
			&foreignKey.Schema,
			&foreignKey.Resource,
			&foreignKey.Name,
			&foreignKey.Column,
			&foreignKey.ReferencedSchema,
			&foreignKey.ReferencedTable,
			&foreignKey.ReferencedColumn,
		); err != nil {
			return nil, fmt.Errorf("scan foreign key: %w", err)
		}
		foreignKeys = append(foreignKeys, foreignKey)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read foreign keys: %w", err)
	}
	return foreignKeys, nil
}

const objectsQuery = `
SELECT
    n.nspname,
    c.relname,
    CASE WHEN c.relkind IN ('v', 'm') THEN 'view' ELSE 'table' END
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND n.nspname <> 'information_schema'
  AND n.nspname !~ '^pg_'
ORDER BY n.nspname, c.relname`

const columnsQuery = `
SELECT
    n.nspname,
    c.relname,
    a.attname,
    pg_catalog.format_type(a.atttypid, a.atttypmod),
    NOT a.attnotnull,
    pg_catalog.pg_get_expr(d.adbin, d.adrelid),
    CASE a.attidentity WHEN 'a' THEN 'always' WHEN 'd' THEN 'by_default' ELSE '' END,
    a.attgenerated <> '',
    COALESCE(
        (SELECT array_agg(e.enumlabel ORDER BY e.enumsortorder)
         FROM pg_catalog.pg_enum e
         WHERE e.enumtypid = a.atttypid),
        '{}'
    )
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND a.attnum > 0
  AND NOT a.attisdropped
  AND n.nspname <> 'information_schema'
  AND n.nspname !~ '^pg_'
ORDER BY n.nspname, c.relname, a.attnum`

const primaryKeysQuery = `
SELECT n.nspname, c.relname, a.attname
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS key(attnum, position)
JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = key.attnum
WHERE con.contype = 'p'
  AND n.nspname <> 'information_schema'
  AND n.nspname !~ '^pg_'
ORDER BY n.nspname, c.relname, key.position`

const foreignKeysQuery = `
SELECT
    n.nspname,
    c.relname,
    con.conname,
    a.attname,
    referenced_n.nspname,
    referenced_c.relname,
    referenced_a.attname
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_class referenced_c ON referenced_c.oid = con.confrelid
JOIN pg_catalog.pg_namespace referenced_n ON referenced_n.oid = referenced_c.relnamespace
CROSS JOIN LATERAL unnest(con.conkey, con.confkey) WITH ORDINALITY AS key(attnum, referenced_attnum, position)
JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = key.attnum
JOIN pg_catalog.pg_attribute referenced_a
    ON referenced_a.attrelid = referenced_c.oid AND referenced_a.attnum = key.referenced_attnum
WHERE con.contype = 'f'
  AND n.nspname <> 'information_schema'
  AND n.nspname !~ '^pg_'
  AND referenced_n.nspname <> 'information_schema'
  AND referenced_n.nspname !~ '^pg_'
ORDER BY n.nspname, c.relname, con.conname, key.position`

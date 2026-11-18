package records

import (
	"fmt"
	"strings"

	"velmir/backend/internal/introspection"
)

var comparisonOperators = map[string]string{
	"eq":  "=",
	"neq": "<>",
	"lt":  "<",
	"lte": "<=",
	"gt":  ">",
	"gte": ">=",
}

func BuildSelectQuery(resource introspection.Resource, schemaName string, options ListOptions) (string, []any) {
	columns := make([]string, 0, len(resource.Columns))
	for _, column := range resource.Columns {
		columns = append(columns, quoteIdentifier(column.Name))
	}
	selectList := "*"
	if len(columns) > 0 {
		selectList = strings.Join(columns, ", ")
	}

	var query strings.Builder
	query.WriteString("SELECT ")
	query.WriteString(selectList)
	query.WriteString(" FROM ")
	query.WriteString(quoteIdentifier(schemaName))
	query.WriteByte('.')
	query.WriteString(quoteIdentifier(resource.Name))

	args := make([]any, 0, len(options.Filters)+3)
	conditions := make([]string, 0, len(options.Filters)+1)
	for _, filter := range options.Filters {
		column := quoteIdentifier(filter.Column)
		if filter.Operator == "is_null" {
			if filter.NotNull {
				conditions = append(conditions, column+" IS NOT NULL")
			} else {
				conditions = append(conditions, column+" IS NULL")
			}
			continue
		}

		args = append(args, filter.Value)
		placeholder := fmt.Sprintf("$%d", len(args))
		if baseType(filterType(resource, filter.Column)) == "numeric" || baseType(filterType(resource, filter.Column)) == "decimal" {
			placeholder += "::numeric"
		} else if baseType(filterType(resource, filter.Column)) == "uuid" {
			placeholder += "::uuid"
		}
		if filter.Operator == "contains" {
			args[len(args)-1] = "%" + escapeLike(filter.Value.(string)) + "%"
			conditions = append(conditions, column+" ILIKE "+placeholder+" ESCAPE '\\'")
		} else {
			conditions = append(conditions, column+" "+comparisonOperators[filter.Operator]+" "+placeholder)
		}
	}

	if options.Search != "" {
		args = append(args, "%"+escapeLike(options.Search)+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		searchConditions := make([]string, 0, len(options.SearchColumns))
		for _, column := range options.SearchColumns {
			searchConditions = append(searchConditions, quoteIdentifier(column)+" ILIKE "+placeholder+" ESCAPE '\\'")
		}
		conditions = append(conditions, "("+strings.Join(searchConditions, " OR ")+")")
	}
	if len(conditions) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(conditions, " AND "))
	}

	orderColumns := make([]string, 0, len(resource.PrimaryKey)+1)
	if options.Sort != "" {
		orderColumns = append(orderColumns, quoteIdentifier(options.Sort)+" "+strings.ToUpper(options.Direction))
	}
	for _, primaryKey := range resource.PrimaryKey {
		if primaryKey == options.Sort {
			continue
		}
		orderColumns = append(orderColumns, quoteIdentifier(primaryKey)+" "+strings.ToUpper(options.Direction))
	}
	if len(orderColumns) > 0 {
		query.WriteString(" ORDER BY ")
		query.WriteString(strings.Join(orderColumns, ", "))
	}

	args = append(args, options.Limit+1, (options.Page-1)*int64(options.Limit))
	query.WriteString(fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)))
	return query.String(), args
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func filterType(resource introspection.Resource, columnName string) string {
	column, _ := findColumn(resource, columnName)
	return column.DataType
}

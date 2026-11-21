package authorization

import (
	"errors"
	"fmt"

	"velmir/backend/internal/introspection"
)

func ValidatePermissionColumns(permissions Permissions, catalog introspection.Catalog) error {
	for _, permission := range permissions {
		if len(permission.FieldPermissions) == 0 {
			continue
		}
		if permission.Schema == "*" || permission.Resource == "*" {
			return errors.New("field allowlists require a concrete schema and resource")
		}
		resource, ok := catalog.FindResource(permission.Schema, permission.Resource)
		if !ok {
			return fmt.Errorf("field permissions require resource %s.%s to exist in target metadata", permission.Schema, permission.Resource)
		}
		for _, fieldPermission := range permission.FieldPermissions {
			for _, name := range fieldPermission.Columns {
				column, ok := findColumn(resource, name)
				if !ok {
					return fmt.Errorf("field allowlist column %s is not present in target metadata", name)
				}
				if fieldPermission.Action != Read && (!column.Editable || column.Generated || column.Identity != "" || column.PrimaryKey) {
					return fmt.Errorf("field allowlist column %s is not writable", name)
				}
			}
		}
	}
	return nil
}

func findColumn(resource introspection.Resource, name string) (introspection.Column, bool) {
	for _, column := range resource.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return introspection.Column{}, false
}

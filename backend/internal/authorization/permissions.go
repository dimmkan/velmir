package authorization

import (
	"errors"
	"sort"
	"strings"
)

type Operation string

const (
	Read   Operation = "read"
	Create Operation = "create"
	Update Operation = "update"
	Delete Operation = "delete"
)

type Permission struct {
	Schema     string      `json:"schema"`
	Resource   string      `json:"resource"`
	Operations []Operation `json:"operations"`
}

type Permissions []Permission

type Grant struct {
	Schema    string
	Resource  string
	Operation Operation
}

func EffectivePermissions(grants []Grant) Permissions {
	operations := make(map[[2]string]map[Operation]struct{})
	for _, grant := range grants {
		if !validOperation(grant.Operation) {
			continue
		}
		key := [2]string{grant.Schema, grant.Resource}
		if operations[key] == nil {
			operations[key] = make(map[Operation]struct{})
		}
		operations[key][grant.Operation] = struct{}{}
	}

	keys := make([][2]string, 0, len(operations))
	for key := range operations {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] == keys[j][0] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})

	permissions := make(Permissions, 0, len(keys))
	for _, key := range keys {
		permission := Permission{Schema: key[0], Resource: key[1], Operations: make([]Operation, 0, 4)}
		for _, operation := range []Operation{Read, Create, Update, Delete} {
			if _, ok := operations[key][operation]; ok {
				permission.Operations = append(permission.Operations, operation)
			}
		}
		permissions = append(permissions, permission)
	}
	return permissions
}

func (permissions Permissions) Allows(schema, resource string, operation Operation) bool {
	if !validOperation(operation) {
		return false
	}
	for _, permission := range permissions {
		if !matches(permission.Schema, schema) || !matches(permission.Resource, resource) {
			continue
		}
		for _, granted := range permission.Operations {
			if granted == operation {
				return true
			}
		}
	}
	return false
}

func (permissions Permissions) HasAny(operations ...Operation) bool {
	for _, operation := range operations {
		if permissions.Allows("*", "*", operation) {
			return true
		}
		for _, permission := range permissions {
			for _, granted := range permission.Operations {
				if granted == operation {
					return true
				}
			}
		}
	}
	return false
}

func matches(pattern, value string) bool {
	return pattern == "*" || pattern == value
}

func ValidatePermissions(permissions Permissions) error {
	if len(permissions) > 1000 {
		return errors.New("a role cannot contain more than 1000 permission entries")
	}
	pairs := make(map[[2]string]struct{}, len(permissions))
	for _, permission := range permissions {
		schemaName := strings.TrimSpace(permission.Schema)
		resourceName := strings.TrimSpace(permission.Resource)
		if schemaName == "" || resourceName == "" || len(schemaName) > 255 || len(resourceName) > 255 {
			return errors.New("each permission requires a schema and resource of at most 255 characters")
		}
		if (schemaName == "*" || resourceName == "*") && (schemaName != "*" || resourceName != "*") {
			return errors.New("wildcard permissions must use * for both schema and resource")
		}
		key := [2]string{schemaName, resourceName}
		if _, exists := pairs[key]; exists {
			return errors.New("duplicate schema and resource permission entries are not allowed")
		}
		pairs[key] = struct{}{}
		if len(permission.Operations) == 0 {
			return errors.New("each permission requires at least one operation")
		}
		seen := make(map[Operation]struct{}, len(permission.Operations))
		for _, operation := range permission.Operations {
			if !validOperation(operation) {
				return errors.New("permissions support only read, create, update and delete operations")
			}
			if _, exists := seen[operation]; exists {
				return errors.New("duplicate operations are not allowed")
			}
			seen[operation] = struct{}{}
		}
	}
	return nil
}

func validOperation(operation Operation) bool {
	return operation == Read || operation == Create || operation == Update || operation == Delete
}

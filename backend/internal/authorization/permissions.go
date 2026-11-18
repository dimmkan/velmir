package authorization

import "sort"

type Operation string

const (
	Read   Operation = "read"
	Create Operation = "create"
	Update Operation = "update"
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
		permission := Permission{Schema: key[0], Resource: key[1], Operations: make([]Operation, 0, 3)}
		for _, operation := range []Operation{Read, Create, Update} {
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

func validOperation(operation Operation) bool {
	return operation == Read || operation == Create || operation == Update
}

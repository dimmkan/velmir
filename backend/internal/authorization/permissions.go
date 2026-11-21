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

type FieldPermissionMode string

const (
	FieldUnrestricted FieldPermissionMode = "unrestricted"
	FieldAllowlist    FieldPermissionMode = "allowlist"
)

const MaxFieldPermissionColumns = 1000

type FieldPermission struct {
	Action  Operation           `json:"action"`
	Mode    FieldPermissionMode `json:"mode"`
	Columns []string            `json:"columns"`
}

type Permission struct {
	Schema           string            `json:"schema"`
	Resource         string            `json:"resource"`
	Operations       []Operation       `json:"operations"`
	FieldPermissions []FieldPermission `json:"field_permissions,omitempty"`
}

type Permissions []Permission

type Grant struct {
	RoleID    int64
	Schema    string
	Resource  string
	Operation Operation
}

type FieldGrant struct {
	RoleID   int64
	Schema   string
	Resource string
	Action   Operation
	Mode     FieldPermissionMode
	Columns  []string
}

type FieldAccess struct {
	Granted      bool
	Unrestricted bool
	Columns      []string
}

func (access FieldAccess) Allows(column string) bool {
	if !access.Granted {
		return false
	}
	if access.Unrestricted {
		return true
	}
	for _, allowed := range access.Columns {
		if allowed == column {
			return true
		}
	}
	return false
}

func EffectivePermissions(grants []Grant) Permissions {
	return effectivePermissions(grants, nil, false)
}

func EffectivePermissionsWithFields(grants []Grant, fieldGrants []FieldGrant) Permissions {
	return effectivePermissions(grants, fieldGrants, true)
}

func effectivePermissions(grants []Grant, fieldGrants []FieldGrant, includeFields bool) Permissions {
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
		if includeFields && key[0] != "*" && key[1] != "*" {
			for _, operation := range []Operation{Create, Update} {
				if _, ok := operations[key][operation]; !ok {
					continue
				}
				access := exactFieldAccess(grants, fieldGrants, key[0], key[1], operation)
				columns := access.Columns
				if columns == nil {
					columns = make([]string, 0)
				}
				mode := FieldAllowlist
				if access.Unrestricted {
					mode = FieldUnrestricted
					columns = make([]string, 0)
				}
				permission.FieldPermissions = append(permission.FieldPermissions, FieldPermission{
					Action: operation, Mode: mode, Columns: columns,
				})
			}
		}
		permissions = append(permissions, permission)
	}
	return permissions
}

func exactFieldAccess(grants []Grant, fieldGrants []FieldGrant, schema, resource string, operation Operation) FieldAccess {
	access := FieldAccess{}
	columns := make(map[string]struct{})
	for _, grant := range grants {
		if grant.Schema != schema || grant.Resource != resource || grant.Operation != operation {
			continue
		}
		access.Granted = true
		if schema == "*" && resource == "*" {
			access.Unrestricted = true
			return access
		}
		var configured *FieldGrant
		for index := range fieldGrants {
			fieldGrant := &fieldGrants[index]
			if fieldGrant.RoleID == grant.RoleID && fieldGrant.Schema == schema && fieldGrant.Resource == resource && fieldGrant.Action == operation {
				configured = fieldGrant
				break
			}
		}
		if configured == nil || configured.Mode == FieldUnrestricted {
			access.Unrestricted = true
			access.Columns = nil
			return access
		}
		if configured.Mode != FieldAllowlist {
			continue
		}
		for _, column := range configured.Columns {
			columns[column] = struct{}{}
		}
	}
	access.Columns = sortedColumns(columns)
	return access
}

func (permissions Permissions) EffectiveFields(schema, resource string, operation Operation) FieldAccess {
	access := FieldAccess{}
	if operation != Create && operation != Update {
		return access
	}
	columns := make(map[string]struct{})
	for _, permission := range permissions {
		if !matches(permission.Schema, schema) || !matches(permission.Resource, resource) || !hasOperation(permission, operation) {
			continue
		}
		access.Granted = true
		if permission.Schema == "*" && permission.Resource == "*" {
			access.Unrestricted = true
			access.Columns = nil
			return access
		}
		var configured *FieldPermission
		for index := range permission.FieldPermissions {
			if permission.FieldPermissions[index].Action == operation {
				configured = &permission.FieldPermissions[index]
				break
			}
		}
		if configured == nil || configured.Mode == FieldUnrestricted {
			access.Unrestricted = true
			access.Columns = nil
			return access
		}
		if configured.Mode != FieldAllowlist {
			continue
		}
		for _, column := range configured.Columns {
			columns[column] = struct{}{}
		}
	}
	access.Columns = sortedColumns(columns)
	return access
}

func (permissions Permissions) Allows(schema, resource string, operation Operation) bool {
	if !validOperation(operation) {
		return false
	}
	for _, permission := range permissions {
		if !matches(permission.Schema, schema) || !matches(permission.Resource, resource) {
			continue
		}
		if hasOperation(permission, operation) {
			return true
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

func NormalizePermissions(permissions Permissions) Permissions {
	normalized := make(Permissions, 0, len(permissions))
	for _, permission := range permissions {
		copyPermission := Permission{
			Schema: permission.Schema, Resource: permission.Resource,
			Operations:       append([]Operation(nil), permission.Operations...),
			FieldPermissions: make([]FieldPermission, 0, len(permission.FieldPermissions)+2),
		}
		for _, fieldPermission := range permission.FieldPermissions {
			var columns []string
			if fieldPermission.Columns != nil {
				columns = append([]string{}, fieldPermission.Columns...)
			}
			if fieldPermission.Mode == FieldUnrestricted || columns == nil && fieldPermission.Mode != FieldAllowlist {
				columns = make([]string, 0)
			}
			copyPermission.FieldPermissions = append(copyPermission.FieldPermissions, FieldPermission{
				Action: fieldPermission.Action, Mode: fieldPermission.Mode, Columns: columns,
			})
		}
		if permission.Schema != "*" && permission.Resource != "*" {
			for _, operation := range []Operation{Create, Update} {
				if !hasOperation(permission, operation) || hasFieldPermission(permission, operation) {
					continue
				}
				copyPermission.FieldPermissions = append(copyPermission.FieldPermissions, FieldPermission{
					Action: operation, Mode: FieldUnrestricted, Columns: make([]string, 0),
				})
			}
		}
		sort.Slice(copyPermission.FieldPermissions, func(i, j int) bool {
			return copyPermission.FieldPermissions[i].Action < copyPermission.FieldPermissions[j].Action
		})
		normalized = append(normalized, copyPermission)
	}
	return normalized
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
		if schemaName == "*" && len(permission.FieldPermissions) > 0 {
			return errors.New("wildcard permissions are always unrestricted and cannot have field restrictions")
		}
		if len(permission.FieldPermissions) > 2 {
			return errors.New("field permissions support only create and update actions")
		}
		fieldActions := make(map[Operation]struct{}, len(permission.FieldPermissions))
		for _, fieldPermission := range permission.FieldPermissions {
			if fieldPermission.Action != Create && fieldPermission.Action != Update {
				return errors.New("field permissions support only create and update actions")
			}
			if _, ok := seen[fieldPermission.Action]; !ok {
				return errors.New("a field permission requires the corresponding resource operation")
			}
			if _, exists := fieldActions[fieldPermission.Action]; exists {
				return errors.New("duplicate field permission actions are not allowed")
			}
			fieldActions[fieldPermission.Action] = struct{}{}
			if fieldPermission.Mode != FieldUnrestricted && fieldPermission.Mode != FieldAllowlist {
				return errors.New("field permission mode must be unrestricted or allowlist")
			}
			if fieldPermission.Mode == FieldUnrestricted {
				if len(fieldPermission.Columns) != 0 {
					return errors.New("unrestricted field permissions cannot contain columns")
				}
				continue
			}
			if fieldPermission.Columns == nil {
				return errors.New("an allowlist must contain an explicit columns array")
			}
			if len(fieldPermission.Columns) > MaxFieldPermissionColumns {
				return errors.New("a field allowlist cannot contain more than 1000 columns")
			}
			columns := make(map[string]struct{}, len(fieldPermission.Columns))
			for _, column := range fieldPermission.Columns {
				if strings.TrimSpace(column) == "" || len(column) > 255 {
					return errors.New("each field allowlist column must be non-empty and at most 255 characters")
				}
				if _, exists := columns[column]; exists {
					return errors.New("duplicate field allowlist columns are not allowed")
				}
				columns[column] = struct{}{}
			}
		}
	}
	return nil
}

func hasOperation(permission Permission, operation Operation) bool {
	for _, granted := range permission.Operations {
		if granted == operation {
			return true
		}
	}
	return false
}

func hasFieldPermission(permission Permission, operation Operation) bool {
	for _, fieldPermission := range permission.FieldPermissions {
		if fieldPermission.Action == operation {
			return true
		}
	}
	return false
}

func sortedColumns(columns map[string]struct{}) []string {
	result := make([]string, 0, len(columns))
	for column := range columns {
		result = append(result, column)
	}
	sort.Strings(result)
	return result
}

func matches(pattern, value string) bool {
	return pattern == "*" || pattern == value
}

func validOperation(operation Operation) bool {
	return operation == Read || operation == Create || operation == Update || operation == Delete
}

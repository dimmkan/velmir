package authorization

import (
	"reflect"
	"testing"

	"velmir/backend/internal/introspection"
)

func TestFieldPermissionUnionSemantics(t *testing.T) {
	grants := []Grant{
		{RoleID: 1, Schema: "public", Resource: "books", Operation: Read},
		{RoleID: 1, Schema: "public", Resource: "books", Operation: Create},
		{RoleID: 1, Schema: "public", Resource: "books", Operation: Update},
		{RoleID: 2, Schema: "public", Resource: "books", Operation: Update},
		{RoleID: 3, Schema: "public", Resource: "books", Operation: Update},
	}
	fields := []FieldGrant{
		{RoleID: 1, Schema: "public", Resource: "books", Action: Create, Mode: FieldAllowlist, Columns: []string{}},
		{RoleID: 1, Schema: "public", Resource: "books", Action: Update, Mode: FieldAllowlist, Columns: []string{"title"}},
		{RoleID: 2, Schema: "public", Resource: "books", Action: Update, Mode: FieldAllowlist, Columns: []string{}},
		{RoleID: 3, Schema: "public", Resource: "books", Action: Update, Mode: FieldAllowlist, Columns: []string{"summary", "title"}},
	}
	permissions := EffectivePermissionsWithFields(grants, fields)
	if access := permissions.EffectiveFields("public", "books", Create); !access.Granted || access.Unrestricted || len(access.Columns) != 0 {
		t.Fatalf("empty create allowlist = %+v", access)
	}
	if access := permissions.EffectiveFields("public", "books", Update); access.Unrestricted || !reflect.DeepEqual(access.Columns, []string{"summary", "title"}) {
		t.Fatalf("unioned update allowlist = %+v", access)
	}

	fields[1].Mode = FieldUnrestricted
	permissions = EffectivePermissionsWithFields(grants, fields)
	if access := permissions.EffectiveFields("public", "books", Update); !access.Unrestricted {
		t.Fatalf("unrestricted plus restricted = %+v", access)
	}
}

func TestWildcardCannotBeNarrowed(t *testing.T) {
	permissions := EffectivePermissionsWithFields([]Grant{
		{RoleID: 1, Schema: "*", Resource: "*", Operation: Update},
		{RoleID: 2, Schema: "public", Resource: "books", Operation: Update},
	}, []FieldGrant{
		{RoleID: 2, Schema: "public", Resource: "books", Action: Update, Mode: FieldAllowlist, Columns: []string{}},
	})
	if access := permissions.EffectiveFields("public", "books", Update); !access.Unrestricted {
		t.Fatalf("wildcard was narrowed: %+v", access)
	}
	if err := ValidatePermissions(Permissions{{
		Schema: "*", Resource: "*", Operations: []Operation{Update},
		FieldPermissions: []FieldPermission{{Action: Update, Mode: FieldAllowlist, Columns: []string{}}},
	}}); err == nil {
		t.Fatal("wildcard field restriction was accepted")
	}
}

func TestFieldPermissionStructureValidation(t *testing.T) {
	base := Permission{Schema: "public", Resource: "books", Operations: []Operation{Create, Update}}
	for _, field := range []FieldPermission{
		{Action: Read, Mode: FieldAllowlist, Columns: []string{}},
		{Action: Create, Mode: FieldPermissionMode("unknown"), Columns: []string{}},
		{Action: Create, Mode: FieldAllowlist},
		{Action: Create, Mode: FieldAllowlist, Columns: []string{"title", "title"}},
		{Action: Update, Mode: FieldUnrestricted, Columns: []string{"title"}},
	} {
		permission := base
		permission.FieldPermissions = []FieldPermission{field}
		if err := ValidatePermissions(Permissions{permission}); err == nil {
			t.Errorf("invalid field permission accepted: %+v", field)
		}
	}
	if err := ValidatePermissions(Permissions{{
		Schema: "public", Resource: "books", Operations: []Operation{Update},
		FieldPermissions: []FieldPermission{{Action: Update, Mode: FieldAllowlist, Columns: []string{}}},
	}}); err != nil {
		t.Fatalf("explicit empty allowlist rejected: %v", err)
	}
}

func TestFieldPermissionMetadataValidation(t *testing.T) {
	catalog := introspection.Catalog{Schemas: []introspection.Schema{{Name: "public", Resources: []introspection.Resource{{
		Name: "books",
		Columns: []introspection.Column{
			{Name: "id", Editable: true, PrimaryKey: true},
			{Name: "title", Editable: true},
			{Name: "slug", Editable: true, Identity: "by_default"},
			{Name: "search", Generated: true},
			{Name: "internal"},
		},
	}}}}}
	for _, column := range []string{"id", "slug", "search", "internal", "missing"} {
		permissions := Permissions{{
			Schema: "public", Resource: "books", Operations: []Operation{Update},
			FieldPermissions: []FieldPermission{{Action: Update, Mode: FieldAllowlist, Columns: []string{column}}},
		}}
		if err := ValidatePermissionColumns(permissions, catalog); err == nil {
			t.Errorf("non-writable or unknown column %q accepted", column)
		}
	}
	permissions := Permissions{{
		Schema: "public", Resource: "books", Operations: []Operation{Create},
		FieldPermissions: []FieldPermission{{Action: Create, Mode: FieldAllowlist, Columns: []string{"title"}}},
	}}
	if err := ValidatePermissionColumns(permissions, catalog); err != nil {
		t.Fatalf("writable metadata column rejected: %v", err)
	}
}

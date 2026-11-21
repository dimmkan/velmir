package authorization

import (
	"reflect"
	"testing"

	"velmir/backend/internal/introspection"
)

func TestReadFieldPermissionUnionEmptyWildcardAndLegacy(t *testing.T) {
	grants := []Grant{
		{RoleID: 1, Schema: "public", Resource: "books", Operation: Read},
		{RoleID: 2, Schema: "public", Resource: "books", Operation: Read},
	}
	fields := []FieldGrant{
		{RoleID: 1, Schema: "public", Resource: "books", Action: Read, Mode: FieldAllowlist, Columns: []string{}},
		{RoleID: 2, Schema: "public", Resource: "books", Action: Read, Mode: FieldAllowlist, Columns: []string{"title"}},
	}
	permissions := EffectivePermissionsWithFields(grants, fields)
	access := permissions.EffectiveFields("public", "books", Read)
	if !access.Granted || access.Unrestricted || !reflect.DeepEqual(access.Columns, []string{"title"}) {
		t.Fatalf("read union = %+v", access)
	}
	fields[0].Mode = FieldUnrestricted
	permissions = EffectivePermissionsWithFields(grants, fields)
	if access := permissions.EffectiveFields("public", "books", Read); !access.Unrestricted {
		t.Fatalf("unrestricted did not dominate: %+v", access)
	}
	legacy := EffectivePermissions([]Grant{{Schema: "public", Resource: "books", Operation: Read}})
	if access := legacy.EffectiveFields("public", "books", Read); !access.Unrestricted {
		t.Fatalf("legacy read was restricted: %+v", access)
	}
	wildcard := EffectivePermissionsWithFields([]Grant{{Schema: "*", Resource: "*", Operation: Read}}, nil)
	if access := wildcard.EffectiveFields("public", "books", Read); !access.Unrestricted {
		t.Fatalf("wildcard read was restricted: %+v", access)
	}
}

func TestReadAllowlistAcceptsPrimaryKeyGeneratedAndReadOnlyColumns(t *testing.T) {
	catalog := introspection.Catalog{Schemas: []introspection.Schema{{Name: "public", Resources: []introspection.Resource{{
		Name: "books", Columns: []introspection.Column{
			{Name: "id", PrimaryKey: true},
			{Name: "search", Generated: true},
			{Name: "internal"},
		},
	}}}}}
	permissions := Permissions{{
		Schema: "public", Resource: "books", Operations: []Operation{Read},
		FieldPermissions: []FieldPermission{{Action: Read, Mode: FieldAllowlist, Columns: []string{"id", "search", "internal"}}},
	}}
	if err := ValidatePermissions(permissions); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePermissionColumns(permissions, catalog); err != nil {
		t.Fatal(err)
	}
}

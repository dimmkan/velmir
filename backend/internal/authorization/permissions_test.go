package authorization

import (
	"reflect"
	"testing"
)

func TestEffectivePermissionsUnionsRolesAndDefaultsToDeny(t *testing.T) {
	permissions := EffectivePermissions([]Grant{
		{Schema: "public", Resource: "books", Operation: Read},
		{Schema: "public", Resource: "books", Operation: Read},
		{Schema: "public", Resource: "books", Operation: Create},
		{Schema: "sales", Resource: "orders", Operation: Update},
		{Schema: "sales", Resource: "orders", Operation: Delete},
		{Schema: "sales", Resource: "orders", Operation: Operation("publish")},
	})

	want := Permissions{
		{Schema: "public", Resource: "books", Operations: []Operation{Read, Create}},
		{Schema: "sales", Resource: "orders", Operations: []Operation{Update, Delete}},
	}
	if !reflect.DeepEqual(permissions, want) {
		t.Fatalf("permissions = %#v, want %#v", permissions, want)
	}
	if !permissions.Allows("public", "books", Create) || permissions.Allows("public", "books", Update) || permissions.Allows("public", "books", Delete) {
		t.Fatal("resource permissions were not evaluated independently")
	}
	if permissions.Allows("private", "secrets", Read) || permissions.Allows("sales", "orders", Operation("publish")) {
		t.Fatal("missing or unsupported permission was allowed")
	}
}

func TestSystemRoleWildcards(t *testing.T) {
	admin := EffectivePermissions([]Grant{
		{Schema: "*", Resource: "*", Operation: Read},
		{Schema: "*", Resource: "*", Operation: Create},
		{Schema: "*", Resource: "*", Operation: Update},
		{Schema: "*", Resource: "*", Operation: Delete},
	})
	viewer := EffectivePermissions([]Grant{{Schema: "*", Resource: "*", Operation: Read}})

	if !admin.Allows("any_schema", "any_resource", Read) || !admin.Allows("any_schema", "any_resource", Create) || !admin.Allows("any_schema", "any_resource", Update) || !admin.Allows("any_schema", "any_resource", Delete) {
		t.Fatal("admin wildcard does not allow supported operations")
	}
	if !viewer.Allows("any_schema", "any_resource", Read) || viewer.HasAny(Create, Update, Delete) {
		t.Fatal("viewer wildcard is not read-only")
	}
	if !admin.HasAny(Create, Update, Delete) || viewer.HasAny(Create, Delete) {
		t.Fatal("aggregate permissions are incorrect")
	}
}

func TestEmptyAndUnknownRolesGrantNothing(t *testing.T) {
	permissions := EffectivePermissions(nil)
	if len(permissions) != 0 || permissions.Allows("public", "books", Read) || permissions.HasAny(Read) {
		t.Fatal("empty permissions must deny access")
	}
}

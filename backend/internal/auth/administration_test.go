package auth

import (
	"testing"

	"velmir/backend/internal/authorization"
)

func TestRoleAndPermissionValidation(t *testing.T) {
	valid := authorization.Permissions{{Schema: "public", Resource: "books", Operations: []authorization.Operation{authorization.Read, authorization.Update, authorization.Delete}}}
	if _, _, err := validateRole("content-editor", "Editors", valid); err != nil {
		t.Fatalf("valid role rejected: %v", err)
	}
	for _, test := range []struct {
		name        string
		role        string
		permissions authorization.Permissions
	}{
		{name: "empty role name", role: "", permissions: valid},
		{name: "invalid role name", role: "Admin", permissions: valid},
		{name: "unknown operation", role: "editor", permissions: authorization.Permissions{{Schema: "public", Resource: "books", Operations: []authorization.Operation{"publish"}}}},
		{name: "partial wildcard", role: "editor", permissions: authorization.Permissions{{Schema: "*", Resource: "books", Operations: []authorization.Operation{authorization.Read}}}},
		{name: "empty resource", role: "editor", permissions: authorization.Permissions{{Schema: "public", Resource: "", Operations: []authorization.Operation{authorization.Read}}}},
		{name: "duplicate operation", role: "editor", permissions: authorization.Permissions{{Schema: "public", Resource: "books", Operations: []authorization.Operation{authorization.Read, authorization.Read}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := validateRole(test.role, "", test.permissions); err == nil {
				t.Fatal("invalid role was accepted")
			}
		})
	}
}

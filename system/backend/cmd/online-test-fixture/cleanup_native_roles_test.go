package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/addp/common/authorization"
)

func TestCleanupNativeFixturePermissionsAreAssignableFromPublishedCatalog(t *testing.T) {
	catalog, err := authorization.LoadRepositoryAuthorizationCatalog(filepath.Clean("../../../.."))
	if err != nil {
		t.Fatal(err)
	}
	permissions := map[string]authorization.PermissionDescriptor{}
	for _, permission := range catalog.Permissions {
		permissions[permission.Key] = permission
	}
	roles := map[string]authorization.BuiltinRoleDescriptor{}
	for _, role := range catalog.Roles {
		roles[role.Key] = role
	}
	for _, subject := range cleanupNativeFixtureSubjects {
		t.Run(subject.name, func(t *testing.T) {
			effective := slices.Clone(subject.permissions)
			for _, key := range subject.permissions {
				permission, exists := permissions[key]
				if !exists || permission.Status != "active" || !permission.TenantCustomizable || !slices.Contains(permission.AllowedScopeTypes, "tenant") {
					t.Fatalf("custom role cannot assign published permission %s", key)
				}
			}
			for _, key := range subject.builtinRoles {
				role, exists := roles[key]
				if !exists || role.RoleType != "tenant_builtin" || !slices.Equal(role.AllowedScopeTypes, []string{"tenant"}) || !slices.Equal(role.AllowedPrincipalTypes, []string{"user"}) {
					t.Fatalf("fixture requires a published Tenant User role: %s", key)
				}
				effective = append(effective, role.Permissions...)
			}
			expected := transferContinuousPermissions
			if subject.name == "external-online-cleanup-native-engine" {
				expected = cleanupNativeEnginePermissions
			}
			slices.Sort(effective)
			want := slices.Clone(expected)
			slices.Sort(want)
			if !slices.Equal(effective, want) {
				t.Fatalf("effective fixture permissions differ: got %v want %v", effective, want)
			}
		})
	}
}

func TestEngineLifecycleOperatorKeepsFourNonCustomizablePermissions(t *testing.T) {
	catalog, err := authorization.LoadRepositoryAuthorizationCatalog(filepath.Clean("../../../.."))
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"system.cleanup.read", "system.engine.create", "system.engine.delete", "system.engine.read"}
	for _, role := range catalog.Roles {
		if role.Key != "tenant.engine_lifecycle_operator" {
			continue
		}
		if !slices.Equal(role.Permissions, expected) || !slices.Equal(role.AllowedScopeTypes, []string{"tenant"}) || !slices.Equal(role.AllowedPrincipalTypes, []string{"user"}) {
			t.Fatalf("lifecycle template gained permissions or assignment scope: %#v", role)
		}
		for _, permission := range catalog.Permissions {
			if slices.Contains(expected, permission.Key) && (permission.TenantCustomizable || !slices.Equal(permission.AllowedScopeTypes, []string{"tenant"})) {
				t.Fatalf("permission boundary changed: %s", permission.Key)
			}
		}
		return
	}
	t.Fatal("lifecycle role is absent from the published catalog")
}

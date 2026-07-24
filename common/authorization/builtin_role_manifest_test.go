package authorization

import (
	"bytes"
	"strings"
	"testing"
)

const validBuiltinRoleManifest = `schema_version: addp.builtin_roles/v1
manifest_version: 1
roles:
  - key: tenant.data_viewer
    role_type: tenant_builtin
    name_i18n_key: roles.tenant.data_viewer.name
    description_i18n_key: roles.tenant.data_viewer.description
    allowed_scope_types: [tenant, department, project_group]
    allowed_principal_types: [user]
    permissions:
      - manager.data_item.read
`

func TestBuiltinRoleManifestSchemaIsValidJSON(t *testing.T) {
	if err := ValidateEmbeddedBuiltinRoleManifestSchema(); err != nil {
		t.Fatal(err)
	}
	first := BuiltinRoleManifestSchema()
	second := BuiltinRoleManifestSchema()
	first[0] = 'x'
	if bytes.Equal(first, second) {
		t.Fatal("BuiltinRoleManifestSchema returned shared mutable bytes")
	}
}

func TestDecodeBuiltinRoleManifest(t *testing.T) {
	manifest, err := DecodeBuiltinRoleManifest(strings.NewReader(validBuiltinRoleManifest))
	if err != nil {
		t.Fatalf("DecodeBuiltinRoleManifest() error = %v", err)
	}
	if len(manifest.Roles) != 1 || manifest.Roles[0].Key != "tenant.data_viewer" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestDecodeBuiltinRoleManifestRejectsUnknownField(t *testing.T) {
	input := strings.Replace(validBuiltinRoleManifest, "manifest_version: 1", "manifest_version: 1\nunknown: true", 1)
	if _, err := DecodeBuiltinRoleManifest(strings.NewReader(input)); err == nil {
		t.Fatal("DecodeBuiltinRoleManifest() error = nil, want unknown field error")
	}
}

func TestDecodeBuiltinRoleManifestRejectsMultipleDocuments(t *testing.T) {
	input := validBuiltinRoleManifest + "---\n" + validBuiltinRoleManifest
	if _, err := DecodeBuiltinRoleManifest(strings.NewReader(input)); err == nil {
		t.Fatal("DecodeBuiltinRoleManifest() error = nil, want multiple document error")
	}
}

func TestValidateBuiltinRoleManifestRejectsInvalidContracts(t *testing.T) {
	valid := mustDecodeBuiltinRoleManifest(t, validBuiltinRoleManifest)
	tests := []struct {
		name   string
		mutate func(*BuiltinRoleManifest)
	}{
		{name: "schema", mutate: func(m *BuiltinRoleManifest) { m.SchemaVersion = "v2" }},
		{name: "version", mutate: func(m *BuiltinRoleManifest) { m.ManifestVersion = 0 }},
		{name: "key", mutate: func(m *BuiltinRoleManifest) { m.Roles[0].Key = "tenant.data.viewer" }},
		{name: "role type", mutate: func(m *BuiltinRoleManifest) { m.Roles[0].RoleType = "tenant_custom" }},
		{name: "namespace", mutate: func(m *BuiltinRoleManifest) { m.Roles[0].RoleType = "platform_builtin" }},
		{name: "name i18n", mutate: func(m *BuiltinRoleManifest) { m.Roles[0].NameI18nKey = "roles.wrong.name" }},
		{name: "description i18n", mutate: func(m *BuiltinRoleManifest) { m.Roles[0].DescriptionI18nKey = "roles.wrong.description" }},
		{name: "scope", mutate: func(m *BuiltinRoleManifest) { m.Roles[0].AllowedScopeTypes = []string{"department", "tenant"} }},
		{name: "principal type", mutate: func(m *BuiltinRoleManifest) { m.Roles[0].AllowedPrincipalTypes = []string{"robot"} }},
		{name: "permission key", mutate: func(m *BuiltinRoleManifest) { m.Roles[0].Permissions[0] = "manager.read" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneBuiltinRoleManifest(valid)
			test.mutate(&manifest)
			if err := ValidateBuiltinRoleManifest(manifest); err == nil {
				t.Fatal("ValidateBuiltinRoleManifest() error = nil")
			}
		})
	}
}

func TestValidateBuiltinRoleManifestRequiresSortedRolesAndPermissions(t *testing.T) {
	manifest := mustDecodeBuiltinRoleManifest(t, validBuiltinRoleManifest)
	second := cloneBuiltinRoleDefinition(manifest.Roles[0])
	second.Key = "tenant.ai_user"
	second.NameI18nKey = "roles.tenant.ai_user.name"
	second.DescriptionI18nKey = "roles.tenant.ai_user.description"
	manifest.Roles = append(manifest.Roles, second)
	if err := ValidateBuiltinRoleManifest(manifest); err == nil {
		t.Fatal("ValidateBuiltinRoleManifest() error = nil, want role sorting error")
	}

	manifest = mustDecodeBuiltinRoleManifest(t, validBuiltinRoleManifest)
	manifest.Roles[0].Permissions = append(manifest.Roles[0].Permissions, "manager.content.read")
	if err := ValidateBuiltinRoleManifest(manifest); err == nil {
		t.Fatal("ValidateBuiltinRoleManifest() error = nil, want permission sorting error")
	}
}

func TestValidatePlatformGovernanceRolesAllowOnlyUsers(t *testing.T) {
	input := strings.ReplaceAll(validBuiltinRoleManifest, "tenant.data_viewer", "platform.system_administrator")
	input = strings.Replace(input, "tenant_builtin", "platform_builtin", 1)
	input = strings.Replace(input, "[tenant, department, project_group]", "[platform]", 1)
	input = strings.Replace(input, "[user]", "[user, service_principal]", 1)

	if _, err := DecodeBuiltinRoleManifest(strings.NewReader(input)); err == nil {
		t.Fatal("DecodeBuiltinRoleManifest() error = nil, want governance principal type error")
	}
}

func TestResolveBuiltinRoles(t *testing.T) {
	manifest := mustDecodeBuiltinRoleManifest(t, validBuiltinRoleManifest)
	permissions := []PermissionDescriptor{{
		Key:               "manager.data_item.read",
		OwnerModule:       "manager",
		AllowedScopeTypes: []string{"tenant", "department", "project_group"},
		Status:            "active",
	}}

	descriptors, err := ResolveBuiltinRoles(manifest, permissions)
	if err != nil {
		t.Fatalf("ResolveBuiltinRoles() error = %v", err)
	}
	if len(descriptors) != 1 || descriptors[0].Key != "tenant.data_viewer" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	manifest.Roles[0].Permissions[0] = "changed"
	if descriptors[0].Permissions[0] != "manager.data_item.read" {
		t.Fatal("descriptor permissions alias source manifest")
	}
}

func TestResolveBuiltinRolesRejectsInvalidPermissionReferences(t *testing.T) {
	valid := mustDecodeBuiltinRoleManifest(t, validBuiltinRoleManifest)
	basePermission := PermissionDescriptor{
		Key:               "manager.data_item.read",
		OwnerModule:       "manager",
		AllowedScopeTypes: []string{"tenant", "department", "project_group"},
		Status:            "active",
	}
	tests := []struct {
		name        string
		manifest    BuiltinRoleManifest
		permissions []PermissionDescriptor
	}{
		{name: "missing", manifest: valid, permissions: []PermissionDescriptor{}},
		{name: "disabled", manifest: valid, permissions: []PermissionDescriptor{func() PermissionDescriptor {
			p := basePermission
			p.Status = "disabled"
			return p
		}()}},
		{name: "scope", manifest: valid, permissions: []PermissionDescriptor{func() PermissionDescriptor {
			p := basePermission
			p.AllowedScopeTypes = []string{"tenant"}
			return p
		}()}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ResolveBuiltinRoles(test.manifest, test.permissions); err == nil {
				t.Fatal("ResolveBuiltinRoles() error = nil")
			}
		})
	}
}

func TestResolveBuiltinRolesRejectsResourceGrantPermissionForUserRole(t *testing.T) {
	input := strings.Replace(validBuiltinRoleManifest, "manager.data_item.read", "manager.resource_grant.read", 1)
	manifest := mustDecodeBuiltinRoleManifest(t, input)
	permissions := []PermissionDescriptor{{
		Key:               "manager.resource_grant.read",
		OwnerModule:       "manager",
		AllowedScopeTypes: []string{"tenant", "department", "project_group"},
		Status:            "active",
	}}

	if _, err := ResolveBuiltinRoles(manifest, permissions); err == nil {
		t.Fatal("ResolveBuiltinRoles() error = nil, want internal permission principal type error")
	}
}

func mustDecodeBuiltinRoleManifest(t *testing.T, input string) BuiltinRoleManifest {
	t.Helper()
	manifest, err := DecodeBuiltinRoleManifest(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func cloneBuiltinRoleManifest(source BuiltinRoleManifest) BuiltinRoleManifest {
	clone := source
	clone.Roles = make([]BuiltinRoleDefinition, len(source.Roles))
	for i := range source.Roles {
		clone.Roles[i] = cloneBuiltinRoleDefinition(source.Roles[i])
	}
	return clone
}

func cloneBuiltinRoleDefinition(source BuiltinRoleDefinition) BuiltinRoleDefinition {
	clone := source
	clone.AllowedScopeTypes = append([]string(nil), source.AllowedScopeTypes...)
	clone.AllowedPrincipalTypes = append([]string(nil), source.AllowedPrincipalTypes...)
	clone.Permissions = append([]string(nil), source.Permissions...)
	return clone
}

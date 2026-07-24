package authorization

import (
	"bytes"
	"strings"
	"testing"
)

const validManagerManifest = `schema_version: addp.permission_manifest/v1
owner_module: manager
manifest_version: 1
permissions:
  - key: manager.data_item.read
    allowed_scope_types: [tenant, department, project_group]
    risk_level: low
    tenant_customizable: true
    delegable: true
    status: active
    name_i18n_key: permissions.manager.data_item.read.name
    description_i18n_key: permissions.manager.data_item.read.description
`

func TestPermissionManifestSchemaIsValidJSON(t *testing.T) {
	if err := ValidateEmbeddedPermissionManifestSchema(); err != nil {
		t.Fatal(err)
	}
	first := PermissionManifestSchema()
	second := PermissionManifestSchema()
	first[0] = 'x'
	if bytes.Equal(first, second) {
		t.Fatal("PermissionManifestSchema returned shared mutable bytes")
	}
}

func TestDecodePermissionManifest(t *testing.T) {
	manifest, err := DecodePermissionManifest(strings.NewReader(validManagerManifest))
	if err != nil {
		t.Fatalf("DecodePermissionManifest() error = %v", err)
	}
	if manifest.OwnerModule != "manager" || len(manifest.Permissions) != 1 {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestDecodePermissionManifestRejectsUnknownField(t *testing.T) {
	input := strings.Replace(validManagerManifest, "manifest_version: 1", "manifest_version: 1\nunknown: true", 1)
	if _, err := DecodePermissionManifest(strings.NewReader(input)); err == nil {
		t.Fatal("DecodePermissionManifest() error = nil, want unknown field error")
	}
}

func TestDecodePermissionManifestRejectsMultipleDocuments(t *testing.T) {
	input := validManagerManifest + "---\n" + validManagerManifest
	if _, err := DecodePermissionManifest(strings.NewReader(input)); err == nil {
		t.Fatal("DecodePermissionManifest() error = nil, want multiple document error")
	}
}

func TestValidatePermissionManifestRejectsInvalidContracts(t *testing.T) {
	valid, err := DecodePermissionManifest(strings.NewReader(validManagerManifest))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*PermissionManifest)
	}{
		{name: "schema", mutate: func(m *PermissionManifest) { m.SchemaVersion = "v2" }},
		{name: "version", mutate: func(m *PermissionManifest) { m.ManifestVersion = 0 }},
		{name: "owner", mutate: func(m *PermissionManifest) { m.OwnerModule = "Manager" }},
		{name: "namespace", mutate: func(m *PermissionManifest) { m.Permissions[0].Key = "meta.data_item.read" }},
		{name: "action", mutate: func(m *PermissionManifest) { m.Permissions[0].Key = "manager.data_item.manage" }},
		{name: "risk", mutate: func(m *PermissionManifest) { m.Permissions[0].RiskLevel = "severe" }},
		{name: "status", mutate: func(m *PermissionManifest) { m.Permissions[0].Status = "removed" }},
		{name: "scopes", mutate: func(m *PermissionManifest) { m.Permissions[0].AllowedScopeTypes = []string{"department", "tenant"} }},
		{name: "platform scope", mutate: func(m *PermissionManifest) {
			m.Permissions[0].AllowedScopeTypes = []string{"platform"}
			m.Permissions[0].TenantCustomizable = false
		}},
		{name: "customizable", mutate: func(m *PermissionManifest) {
			m.Permissions[0].AllowedScopeTypes = []string{"platform"}
			m.OwnerModule = "system"
			m.Permissions[0].Key = "system.data_item.read"
		}},
		{name: "name i18n", mutate: func(m *PermissionManifest) { m.Permissions[0].NameI18nKey = "permissions.wrong.name" }},
		{name: "description i18n", mutate: func(m *PermissionManifest) { m.Permissions[0].DescriptionI18nKey = "permissions.wrong.description" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneManifest(valid)
			test.mutate(&manifest)
			if err := ValidatePermissionManifest(manifest); err == nil {
				t.Fatal("ValidatePermissionManifest() error = nil")
			}
		})
	}
}

func TestValidatePermissionManifestRequiresSortedPermissions(t *testing.T) {
	manifest, err := DecodePermissionManifest(strings.NewReader(validManagerManifest))
	if err != nil {
		t.Fatal(err)
	}
	second := manifest.Permissions[0]
	second.Key = "manager.content.read"
	second.NameI18nKey = "permissions.manager.content.read.name"
	second.DescriptionI18nKey = "permissions.manager.content.read.description"
	manifest.Permissions = append(manifest.Permissions, second)

	if err := ValidatePermissionManifest(manifest); err == nil {
		t.Fatal("ValidatePermissionManifest() error = nil, want sorting error")
	}
}

func TestAggregatePermissionManifests(t *testing.T) {
	manager, err := DecodePermissionManifest(strings.NewReader(validManagerManifest))
	if err != nil {
		t.Fatal(err)
	}
	metaYAML := strings.ReplaceAll(validManagerManifest, "manager", "meta")
	meta, err := DecodePermissionManifest(strings.NewReader(metaYAML))
	if err != nil {
		t.Fatal(err)
	}

	descriptors, err := AggregatePermissionManifests([]PermissionManifest{meta, manager})
	if err != nil {
		t.Fatalf("AggregatePermissionManifests() error = %v", err)
	}
	if len(descriptors) != 2 || descriptors[0].Key != "manager.data_item.read" || descriptors[0].Action != "read" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	manager.Permissions[0].AllowedScopeTypes[0] = "platform"
	if descriptors[0].AllowedScopeTypes[0] != "tenant" {
		t.Fatal("descriptor scopes alias source manifest")
	}
}

func TestAggregatePermissionManifestsRejectsDuplicateOwner(t *testing.T) {
	manifest, err := DecodePermissionManifest(strings.NewReader(validManagerManifest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AggregatePermissionManifests([]PermissionManifest{manifest, manifest}); err == nil {
		t.Fatal("AggregatePermissionManifests() error = nil, want duplicate owner error")
	}
}

func cloneManifest(source PermissionManifest) PermissionManifest {
	clone := source
	clone.Permissions = append([]PermissionDefinition(nil), source.Permissions...)
	for i := range clone.Permissions {
		clone.Permissions[i].AllowedScopeTypes = append([]string(nil), source.Permissions[i].AllowedScopeTypes...)
	}
	return clone
}

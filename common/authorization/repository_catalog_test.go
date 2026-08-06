package authorization

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRepositoryAuthorizationCatalogReportIsDeterministic(t *testing.T) {
	root := testRepositoryRoot(t)
	report, err := LoadRepositoryAuthorizationCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != AuthorizationCatalogReportSchemaVersion {
		t.Fatalf("schema version = %q", report.SchemaVersion)
	}
	if len(report.PermissionManifests) != 16 {
		t.Fatalf("manifest count = %d, want 16", len(report.PermissionManifests))
	}
	owners := make([]string, 0, len(report.PermissionManifests))
	for _, manifest := range report.PermissionManifests {
		owners = append(owners, manifest.OwnerModule)
	}
	wantOwners := []string{"agent", "asset", "copilot", "develop", "graph", "inference", "manager", "meta", "model", "monitor", "orchestrator", "quality", "service", "standard", "system", "transfer"}
	if !reflect.DeepEqual(owners, wantOwners) {
		t.Fatalf("manifest owners = %v, want %v", owners, wantOwners)
	}
	if report.BuiltinRoleManifest.ManifestVersion != 35 ||
		report.BuiltinRoleManifest.Path != "system/authorization/builtin_roles.yaml" {
		t.Fatalf("builtin role manifest reference = %#v", report.BuiltinRoleManifest)
	}

	first, err := MarshalAuthorizationCatalogReport(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalAuthorizationCatalogReport(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("catalog report bytes differ for identical input")
	}
	if !json.Valid(first) {
		t.Fatal("catalog report is not valid JSON")
	}
	if bytes.Contains(first, []byte(root)) {
		t.Fatal("catalog report contains absolute repository path")
	}
	if !bytes.Contains(first, []byte("\"path\": \"manager/authorization/permissions.yaml\"")) {
		t.Fatalf("catalog report does not contain repository-relative manifest path")
	}
}

func TestLoadRepositoryAuthorizationCatalogRejectsUnknownOwnerDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "rogue", strings.ReplaceAll(validManagerManifest, "manager", "rogue"))

	if _, err := LoadRepositoryAuthorizationCatalog(root); err == nil ||
		!strings.Contains(err.Error(), "not a stable permission owner") {
		t.Fatalf("error = %v, want stable permission owner error", err)
	}
}

func TestLoadRepositoryAuthorizationCatalogRejectsOwnerDirectoryMismatch(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "manager", strings.ReplaceAll(validManagerManifest, "manager", "meta"))

	if _, err := LoadRepositoryAuthorizationCatalog(root); err == nil ||
		!strings.Contains(err.Error(), "want directory owner") {
		t.Fatalf("error = %v, want directory owner mismatch", err)
	}
}

func TestLoadRepositoryAuthorizationCatalogRequiresExplicitValidRoot(t *testing.T) {
	if _, err := LoadRepositoryAuthorizationCatalog(""); err == nil {
		t.Fatal("empty repository root error = nil")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRepositoryAuthorizationCatalog(file); err == nil {
		t.Fatal("file repository root error = nil")
	}
}

func TestStablePermissionOwnerModulesReturnsCopy(t *testing.T) {
	first := StablePermissionOwnerModules()
	second := StablePermissionOwnerModules()
	first[0] = "changed"
	if second[0] != "agent" {
		t.Fatal("StablePermissionOwnerModules returned shared mutable slice")
	}
}

func TestMarshalAuthorizationCatalogReportCanonicalizesTopLevelCollections(t *testing.T) {
	report := AuthorizationCatalogReport{
		SchemaVersion: AuthorizationCatalogReportSchemaVersion,
		PermissionManifests: []PermissionManifestReference{
			{OwnerModule: "system", Path: "system/authorization/permissions.yaml"},
			{OwnerModule: "manager", Path: "manager/authorization/permissions.yaml"},
		},
		BuiltinRoleManifest: BuiltinRoleManifestReference{
			ManifestVersion: 1,
			Path:            "system/authorization/builtin_roles.yaml",
		},
		Permissions: []PermissionDescriptor{{Key: "system.engine.read"}, {Key: "manager.data_item.read"}},
		Roles:       []BuiltinRoleDescriptor{{Key: "tenant.data_viewer"}, {Key: "platform.statistics_viewer"}},
	}
	data, err := MarshalAuthorizationCatalogReport(report)
	if err != nil {
		t.Fatal(err)
	}
	managerIndex := bytes.Index(data, []byte("\"owner_module\": \"manager\""))
	systemIndex := bytes.Index(data, []byte("\"owner_module\": \"system\""))
	if managerIndex < 0 || systemIndex < 0 || managerIndex >= systemIndex {
		t.Fatalf("manifest references are not canonical: %s", data)
	}
	if report.PermissionManifests[0].OwnerModule != "system" {
		t.Fatal("MarshalAuthorizationCatalogReport mutated source report")
	}
}

func writeTestManifest(t *testing.T, root, owner, content string) {
	t.Helper()
	directory := filepath.Join(root, owner, "authorization")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "permissions.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

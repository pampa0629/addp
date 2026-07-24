package authorization

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildRepositoryAuthorizationCoverageReportValidatesExplicitContracts(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "manager/backend/docs/swagger.json", `{
  "swagger": "2.0",
  "paths": {
    "/items": {
      "get": {
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": ["manager.data_item.read"]
      }
    }
  }
}`)
	writeCoverageFixture(t, root, "common-python/addp_common/tools/manifest.json", `{
  "tools": [{
    "name": "data.search",
    "owner": "manager",
    "auth": {
      "audience": "manager",
      "required_scopes": ["data.search"],
      "required_permissions": ["manager.data_item.read"]
    }
  }]
}`)
	report, err := BuildRepositoryAuthorizationCoverageReport(root, coverageTestCatalog(true))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || len(report.Issues) != 0 {
		t.Fatalf("coverage report = %#v, want complete", report)
	}
	if report.OpenAPI[0].OperationCount != 1 || report.OpenAPI[0].DeclaredAuthOperationCount != 1 {
		t.Fatalf("OpenAPI source = %#v", report.OpenAPI[0])
	}
	if report.ToolManifest.ToolCount != 1 || report.ToolManifest.MappedToolCount != 1 {
		t.Fatalf("Tool Manifest source = %#v", report.ToolManifest)
	}
}

func TestBuildRepositoryAuthorizationCoverageReportReportsMissingDeclarations(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "manager/backend/docs/swagger.json", `{"swagger":"2.0","paths":{"/items":{"get":{}}}}`)
	writeCoverageFixture(t, root, "common-python/addp_common/tools/manifest.json", `{
  "tools": [{
    "name": "data.search",
    "owner": "manager",
    "auth": {"audience": "manager", "required_scopes": ["data.search"]}
  }]
}`)
	report, err := BuildRepositoryAuthorizationCoverageReport(root, coverageTestCatalog(false))
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete {
		t.Fatal("coverage report complete = true, want false")
	}
	wantCodes := map[string]bool{
		"missing_auth_mode":               false,
		"missing_tool_permission_mapping": false,
	}
	for _, issue := range report.Issues {
		if _, ok := wantCodes[issue.Code]; ok {
			wantCodes[issue.Code] = true
		}
	}
	for code, found := range wantCodes {
		if !found {
			t.Fatalf("coverage issue %q not found: %#v", code, report.Issues)
		}
	}
}

func coverageTestCatalog(delegable bool) AuthorizationCatalogReport {
	return AuthorizationCatalogReport{
		SchemaVersion: AuthorizationCatalogReportSchemaVersion,
		PermissionManifests: []PermissionManifestReference{{
			OwnerModule: "manager",
			Path:        "manager/authorization/permissions.yaml",
		}},
		Permissions: []PermissionDescriptor{{
			Key:         "manager.data_item.read",
			OwnerModule: "manager",
			Status:      "active",
			Delegable:   delegable,
		}},
	}
}

func writeCoverageFixture(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

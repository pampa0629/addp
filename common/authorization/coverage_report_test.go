package authorization

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestOntologyRequiresItsOwnOpenAPI(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "system/backend/docs/swagger.json", `{"paths":{}}`)
	source, _, issues := inspectOwnerOpenAPI(root, "ontology", nil)
	if source.Path != "ontology/backend/docs/swagger.json" || len(issues) != 1 || issues[0].Code != "missing_openapi_document" {
		t.Fatalf("missing owner: %+v %+v", source, issues)
	}
	writeCoverageFixture(t, root, "ontology/backend/docs/swagger.json", `{"paths":{"/ontologies/{ontology_id}/revisions/{revision}/publish":{"post":{"x-addp-auth-mode":"permission","x-addp-required-permissions":["ontology.revision.publish"]}}}}`)
	_, referenced, issues := inspectOwnerOpenAPI(root, "ontology", map[string]PermissionDescriptor{"ontology.revision.publish": {Key: "ontology.revision.publish", OwnerModule: "ontology", Status: "active"}})
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	if _, ok := referenced["ontology.revision.publish"]; !ok {
		t.Fatal("owner reference missing")
	}
	_, _, issues = inspectOwnerOpenAPI(root, "manager", nil)
	if len(issues) != 1 || issues[0].Code != "missing_openapi_document" {
		t.Fatal("other missing API was exempted")
	}
}

func TestPrivateFilesPermissionRequiresActualRuntimeOpenAPI(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "system/backend/docs/swagger.json", `{"paths":{}}`)
	writeCoverageFixture(t, root, "common-python/addp_common/tools/manifest.json", `{"tools":[]}`)
	catalog := AuthorizationCatalogReport{
		SchemaVersion:       AuthorizationCatalogReportSchemaVersion,
		PermissionManifests: []PermissionManifestReference{{OwnerModule: "system", Path: "system/authorization/permissions.yaml"}},
		Permissions:         []PermissionDescriptor{{Key: "system.workflow_private_files.execute", OwnerModule: "system", Status: "active"}},
	}
	report, err := BuildRepositoryAuthorizationCoverageReport(root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete {
		t.Fatal("System Swagger alone hid the missing actual Runtime consumer")
	}
	path := "engines/geopython-workflow/private_files_openapi.json"
	actual, err := os.ReadFile(filepath.Join(testRepositoryRoot(t), path))
	if err != nil {
		t.Fatal(err)
	}
	writeCoverageFixture(t, root, path, string(actual))
	report, err = BuildRepositoryAuthorizationCoverageReport(root, catalog)
	if err != nil || !report.Complete {
		t.Fatalf("actual Runtime coverage: %v %+v", err, report.Issues)
	}
	found := false
	for _, source := range report.OpenAPI {
		if source.Path == path {
			found = source.OwnerModule == "system" && source.OperationCount == 7 && source.DeclaredAuthOperationCount == 7
		}
	}
	if !found {
		t.Fatal("seven actual Runtime operations were not checked")
	}
	canonical, err := MarshalAuthorizationCoverageReport(report)
	if err != nil {
		t.Fatal(err)
	}
	report.OpenAPI[0], report.OpenAPI[1] = report.OpenAPI[1], report.OpenAPI[0]
	reordered, err := MarshalAuthorizationCoverageReport(report)
	if err != nil || !bytes.Equal(canonical, reordered) {
		t.Fatal("multiple real consumers of one Permission owner lost canonical ordering", err)
	}
	writeCoverageFixture(t, root, path, `{"paths":{"/api/private-files/{id}":{"get":{}}}}`)
	report, err = BuildRepositoryAuthorizationCoverageReport(root, catalog)
	if err != nil || report.Complete {
		t.Fatal("undeclared actual Runtime guard bypassed reference coverage", err)
	}
}

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

func TestBuildRepositoryAuthorizationCoverageReportRejectsUnreferencedActivePermission(t *testing.T) {
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
	writeCoverageFixture(t, root, "common-python/addp_common/tools/manifest.json", `{"tools":[]}`)
	catalog := coverageTestCatalog(false)
	catalog.Permissions = append(catalog.Permissions, PermissionDescriptor{
		Key:         "manager.data_item.delete",
		OwnerModule: "manager",
		Status:      "active",
	})

	report, err := BuildRepositoryAuthorizationCoverageReport(root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete {
		t.Fatal("coverage report complete = true, want false")
	}
	for _, issue := range report.Issues {
		if issue.Code == "unreferenced_active_permission" && issue.OwnerModule == "manager" {
			return
		}
	}
	t.Fatalf("unreferenced active Permission issue not found: %#v", report.Issues)
}

func TestBuildRepositoryAuthorizationCoverageReportAcceptsConditionalPermissions(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "manager/backend/docs/swagger.json", `{
  "swagger": "2.0",
  "paths": {
    "/items": {
      "post": {
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": ["manager.data_item.read"],
        "x-addp-conditional-permissions": ["manager.data_item.delete"]
      }
    }
  }
}`)
	writeCoverageFixture(t, root, "common-python/addp_common/tools/manifest.json", `{"tools":[]}`)
	catalog := coverageTestCatalog(false)
	catalog.Permissions = append(catalog.Permissions, PermissionDescriptor{
		Key: "manager.data_item.delete", OwnerModule: "manager", Status: "active",
	})

	report, err := BuildRepositoryAuthorizationCoverageReport(root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || len(report.Issues) != 0 {
		t.Fatalf("coverage report = %#v, want complete", report)
	}
}

func TestBuildRepositoryAuthorizationCoverageReportRejectsConditionalPermissionOverlap(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "manager/backend/docs/swagger.json", `{
  "swagger": "2.0",
  "paths": {
    "/items": {
      "post": {
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": ["manager.data_item.read"],
        "x-addp-conditional-permissions": ["manager.data_item.read"]
      }
    }
  }
}`)
	writeCoverageFixture(t, root, "common-python/addp_common/tools/manifest.json", `{"tools":[]}`)
	report, err := BuildRepositoryAuthorizationCoverageReport(root, coverageTestCatalog(false))
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range report.Issues {
		if issue.Code == "overlapping_conditional_permission" {
			return
		}
	}
	t.Fatalf("overlapping conditional Permission issue not found: %#v", report.Issues)
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

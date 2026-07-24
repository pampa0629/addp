package authorization

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateOwnerPermissionConstantsUsesOwnerLocalLanguagePaths(t *testing.T) {
	report := AuthorizationCatalogReport{
		SchemaVersion: AuthorizationCatalogReportSchemaVersion,
		PermissionManifests: []PermissionManifestReference{
			{OwnerModule: "agent", ManifestVersion: 2, Path: "agent/authorization/permissions.yaml"},
			{OwnerModule: "manager", ManifestVersion: 3, Path: "manager/authorization/permissions.yaml"},
		},
		Permissions: []PermissionDescriptor{
			{Key: "manager.data_item.read", OwnerModule: "manager", Status: "active"},
			{Key: "agent.run.read", OwnerModule: "agent", Status: "active"},
			{Key: "agent.run.cancel", OwnerModule: "agent", Status: "active"},
			{Key: "manager.data_item.delete", OwnerModule: "manager", Status: "disabled"},
		},
	}

	files, err := GenerateOwnerPermissionConstants(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("generated file count = %d, want 2", len(files))
	}
	if files[0].Path != "agent/backend/authorization_permissions_generated.py" {
		t.Fatalf("agent generated path = %q", files[0].Path)
	}
	if files[1].Path != "manager/backend/internal/authorization/permissions_generated.go" {
		t.Fatalf("manager generated path = %q", files[1].Path)
	}

	python := string(files[0].Content)
	if !strings.Contains(python, `AGENT_RUN_CANCEL: Final[str] = "agent.run.cancel"`) ||
		!strings.Contains(python, `AGENT_RUN_READ: Final[str] = "agent.run.read"`) {
		t.Fatalf("generated Python constants missing keys:\n%s", python)
	}
	if strings.Index(python, "agent.run.cancel") >= strings.Index(python, "agent.run.read") {
		t.Fatalf("generated Python constants are not sorted:\n%s", python)
	}

	goSource := string(files[1].Content)
	if !strings.Contains(goSource, `PermissionManagerDataItemRead = "manager.data_item.read"`) {
		t.Fatalf("generated Go constants missing key:\n%s", goSource)
	}
	if strings.Contains(goSource, "manager.data_item.delete") {
		t.Fatalf("generated Go constants contain disabled key:\n%s", goSource)
	}
}

func TestGeneratedOwnerPermissionConstantsWriteAndCheckDetectsDrift(t *testing.T) {
	root := t.TempDir()
	files := []GeneratedOwnerConstantFile{
		{
			OwnerModule: "manager",
			Path:        "manager/backend/internal/authorization/permissions_generated.go",
			Content:     []byte("package authorization\n"),
		},
	}
	if err := WriteGeneratedOwnerPermissionConstants(root, files); err != nil {
		t.Fatal(err)
	}
	if err := CheckGeneratedOwnerPermissionConstants(root, files); err != nil {
		t.Fatalf("fresh generated constants check failed: %v", err)
	}

	path := filepath.Join(root, filepath.FromSlash(files[0].Path))
	if err := os.WriteFile(path, []byte("package stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckGeneratedOwnerPermissionConstants(root, files); err == nil || !strings.Contains(err.Error(), "is stale") {
		t.Fatalf("stale generated constants error = %v", err)
	}
}

func TestGeneratedFilePathRejectsRepositoryEscape(t *testing.T) {
	if _, err := generatedFilePath(t.TempDir(), "../outside"); err == nil {
		t.Fatal("generatedFilePath() escape error = nil")
	}
}

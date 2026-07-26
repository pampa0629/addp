package authorization

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateIAMCatalogSeedSQLFromRepositoryIsDeterministic(t *testing.T) {
	report, err := LoadRepositoryAuthorizationCatalog(testRepositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	first, err := GenerateIAMCatalogSeedSQL(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateIAMCatalogSeedSQL(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("IAM catalog seed bytes differ for identical input")
	}
	for _, required := range []string{
		"INSERT INTO system.permissions",
		"INSERT INTO system.roles",
		"INSERT INTO system.role_permissions",
		"INSERT INTO system.role_conflicts",
		"INSERT INTO system.oauth_clients",
		"'addp-cli'",
		"COMMIT;\n",
	} {
		if !bytes.Contains(first, []byte(required)) {
			t.Fatalf("IAM catalog seed does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"ON CONFLICT", "'addp-web'", "INSERT INTO system.principals", "INSERT INTO system.role_assignments"} {
		if bytes.Contains(first, []byte(forbidden)) {
			t.Fatalf("IAM catalog seed contains forbidden text %q", forbidden)
		}
	}
}

func TestWriteAndCheckGeneratedIAMCatalogSeed(t *testing.T) {
	root := t.TempDir()
	want := []byte("BEGIN;\nCOMMIT;\n")
	if err := WriteGeneratedIAMCatalogSeed(root, want); err != nil {
		t.Fatal(err)
	}
	if err := CheckGeneratedIAMCatalogSeed(root, want); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(IAMCatalogSeedRelativePath))
	if err := os.WriteFile(path, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckGeneratedIAMCatalogSeed(root, want); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale seed check error = %v", err)
	}
}

func TestCheckImmutableIAMCatalogSeedRejectsChangedSeed(t *testing.T) {
	root := t.TempDir()
	if err := WriteGeneratedIAMCatalogSeed(root, []byte("changed\n")); err != nil {
		t.Fatal(err)
	}
	if err := CheckImmutableIAMCatalogSeed(root); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("immutable seed check error = %v", err)
	}
}

func TestGenerateIAMCatalogSeedSQLRejectsMissingConflictRole(t *testing.T) {
	report := AuthorizationCatalogReport{
		SchemaVersion: AuthorizationCatalogReportSchemaVersion,
		Permissions:   []PermissionDescriptor{{Key: "platform.tenant.read"}},
		Roles:         []BuiltinRoleDescriptor{{Key: "platform.audit_administrator"}},
	}
	if _, err := GenerateIAMCatalogSeedSQL(report); err == nil || !strings.Contains(err.Error(), "conflict role") {
		t.Fatalf("missing conflict role error = %v", err)
	}
}

func TestSQLTextEscapesSingleQuote(t *testing.T) {
	if got, want := sqlText("owner's"), "'owner''s'"; got != want {
		t.Fatalf("sqlText() = %q, want %q", got, want)
	}
}

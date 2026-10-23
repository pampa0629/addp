package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCatalogDomainEnvironmentExportsAllThreeUserTokensToChild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.env")
	values := consumerEnvironment("catalog-domain-overview", 2, "synthetic-consumer", "unused")
	values["ADDP_ONLINE_CATALOG_READER_ACCESS_TOKEN"] = "synthetic-reader"
	values["ADDP_ONLINE_CATALOG_PREPARER_ACCESS_TOKEN"] = "synthetic-preparer"
	if err := writeEnvironmentFile(path, values); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("identity file must remain owner-only")
	}
	// Exercise the real writer, source and subprocess boundary without logging credentials.
	command := exec.Command("bash", "-c", `source "$1"; exec bash -c '
test "$ADDP_ONLINE_TEST_USER_ACCESS_TOKEN" = synthetic-consumer &&
test "$ADDP_ONLINE_CATALOG_READER_ACCESS_TOKEN" = synthetic-reader &&
test "$ADDP_ONLINE_CATALOG_PREPARER_ACCESS_TOKEN" = synthetic-preparer
'`, "catalog-environment-test", path)
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err := command.Run(); err != nil {
		t.Fatal("three distinct fixture credentials were not exported to the child process")
	}
}

func TestCatalogDomainFixtureUsesThreeMinimalUsers(t *testing.T) {
	permissions, err := suitePermissions("catalog-domain-overview")
	if err != nil || len(permissions) != 23 || needsEngineProvisioner("catalog-domain-overview") {
		t.Fatalf("unexpected consumer privileges: %v %v", permissions, err)
	}
	for _, permission := range permissions {
		if permission == "standard.element.publish" || permission == "system.role.assign" {
			t.Fatalf("consumer has preparation/admin permission: %s", permission)
		}
	}
	if len(catalogDomainPreparerPermissions) != 6 || !contains(catalogDomainPreparerPermissions, "standard.element.publish") {
		t.Fatal("preparer contract changed")
	}
	for _, permission := range catalogDomainPreparerPermissions {
		if permission == "catalog.inventory.read" {
			t.Fatal("preparer has inventory access")
		}
	}
	values := consumerEnvironment("catalog-domain-overview", 2, "token", "password")
	if len(values) != 2 {
		t.Fatal("API-only fixture exports unnecessary browser credentials")
	}
}

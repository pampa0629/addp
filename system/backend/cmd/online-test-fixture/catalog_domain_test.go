package main

import "testing"

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

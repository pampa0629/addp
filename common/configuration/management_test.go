package configuration

import "testing"

func TestValidateManagementDeclaration(t *testing.T) {
	declaration := &ManagementDeclaration{
		SchemaVersion: ManagementSchemaVersion,
		Entries: []ManagementEntry{{
			ID: "manager.configuration", OwnerModule: "manager",
			ScopeTypes:       []string{ScopePlatformOnly},
			FrontendRoute:    "/manager/settings/embedding",
			ReadPermission:   "manager.configuration.read",
			UpdatePermission: "manager.configuration.update",
		}},
	}
	if err := ValidateManagementDeclaration("manager", declaration); err != nil {
		t.Fatalf("ValidateManagementDeclaration() error = %v", err)
	}
	declaration.Entries[0].OwnerModule = "system"
	if err := ValidateManagementDeclaration("manager", declaration); err == nil {
		t.Fatal("ValidateManagementDeclaration() error = nil, want owner mismatch")
	}
	declaration.Entries[0].OwnerModule = "manager"
	declaration.Entries[0].FrontendRoute = "/configuration/manager"
	if err := ValidateManagementDeclaration("manager", declaration); err != nil {
		t.Fatalf("ValidateManagementDeclaration() Console route error = %v", err)
	}
	declaration.Entries[0].FrontendRoute = "/configuration/manager/embedding"
	if err := ValidateManagementDeclaration("manager", declaration); err != nil {
		t.Fatalf("ValidateManagementDeclaration() nested Console route error = %v", err)
	}
}

func TestEntryVisibleInContext(t *testing.T) {
	tests := []struct {
		scope, context string
		want           bool
	}{
		{ScopePlatformOnly, "platform", true},
		{ScopePlatformOnly, "tenant", false},
		{ScopeTenantOnly, "tenant", true},
		{ScopeTenantOnly, "platform", false},
		{ScopePlatformDefaultWithTenantOverride, "platform", true},
		{ScopePlatformDefaultWithTenantOverride, "tenant", true},
	}
	for _, test := range tests {
		entry := ManagementEntry{ScopeTypes: []string{test.scope}}
		if got := EntryVisibleInContext(entry, test.context); got != test.want {
			t.Fatalf("EntryVisibleInContext(%q, %q) = %v, want %v", test.scope, test.context, got, test.want)
		}
	}
}

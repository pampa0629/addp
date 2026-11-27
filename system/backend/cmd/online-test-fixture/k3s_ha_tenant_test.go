package main

import "testing"

func TestK3sHATenantDatabaseBoundary(t *testing.T) {
	const namespace = "addp-ha-test-6ca38689-3567-49a2-9e53-881d6736fe54"
	good := k3sHADatabase{namespace, "addp_test", "platform_runtime", true}
	if !good.matches(namespace) {
		t.Fatal("dedicated local test database rejected")
	}
	for _, d := range []k3sHADatabase{
		{namespace, "addp", good.Role, true}, {"foreign", good.Database, good.Role, true},
		{namespace, good.Database, "addp", true}, {namespace, good.Database, good.Role, false},
		{"default", good.Database, good.Role, true},
	} {
		if d.matches(namespace) || d.matches("default") {
			t.Fatal("foreign target accepted", d)
		}
	}
	for _, value := range []string{"addp-business", "addp-ha-test-", "addp-ha-test-00000000-0000-0000-0000-000000000000"} {
		if k3sHATestNamespace(value) {
			t.Fatal("invalid namespace accepted", value)
		}
	}
}

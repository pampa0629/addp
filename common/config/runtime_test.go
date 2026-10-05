package config

import (
	"os"
	"reflect"
	"testing"
)

func TestBuildServiceURLNormalizesPort(t *testing.T) {
	if got := BuildServiceURL("localhost", ":8185"); got != "http://localhost:8185" {
		t.Fatalf("BuildServiceURL() = %q, want %q", got, "http://localhost:8185")
	}
}

func TestHostNodeIPsAreCanonicalExplicitAddresses(t *testing.T) {
	t.Setenv("ADDP_HOST_NODE_IPS", " 192.0.2.7,2001:0DB8::1,::ffff:192.0.2.7 ")
	got, err := NormalizeHostNodeIPs(RuntimeHostNodeIPs())
	if err != nil || !reflect.DeepEqual(got, []string{"192.0.2.7", "2001:db8::1"}) {
		t.Fatalf("addresses=%v err=%v", got, err)
	}
	for _, invalid := range []string{"", "host-a", "192.0.2.7:80", "192.0.2.0/24", "fe80::1%en0", "[::1]"} {
		if _, err := NormalizeHostNodeIPs([]string{invalid}); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	t.Setenv("ADDP_HOST_NODE_IPS", "")
	unknown, err := NormalizeHostNodeIPs(RuntimeHostNodeIPs())
	if err != nil || unknown == nil || len(unknown) != 0 {
		t.Fatalf("unknown=%v err=%v", unknown, err)
	}
}

func TestRuntimeLogDeploymentStateRejectsAmbiguousSelection(t *testing.T) {
	t.Setenv("ADDP_OBSERVABILITY_LOGS_ENABLED", "true")
	if err := os.Unsetenv("ADDP_OBSERVABILITY_LOGS_ENABLED"); err != nil {
		t.Fatal(err)
	}
	if got := RuntimeLogDeploymentState(); got != "enabled" {
		t.Fatal(got)
	}
	for input, expected := range map[string]string{"true": "enabled", "false": "disabled", "": "unconfigured", "1": "unconfigured", "TRUE": "unconfigured"} {
		t.Setenv("ADDP_OBSERVABILITY_LOGS_ENABLED", input)
		if got := RuntimeLogDeploymentState(); got != expected {
			t.Fatalf("selection %q: %s", input, got)
		}
	}
}

func TestOptionalRuntimeNodeIDDoesNotBlockBusiness(t *testing.T) {
	for _, value := range []string{"", "secret-invalid-value", "00000000-0000-0000-0000-000000000000", "urn:uuid:550e8400-e29b-41d4-a716-446655440000"} {
		t.Setenv("ADDP_HOST_NODE_ID", value)
		if id := RuntimeHostNodeID(); id != "" {
			t.Fatalf("invalid declaration retained: %q", id)
		}
	}
	t.Setenv("ADDP_HOST_NODE_ID", " 550E8400-E29B-41D4-A716-446655440000 ")
	if id := RuntimeHostNodeID(); id != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatal(id)
	}
}

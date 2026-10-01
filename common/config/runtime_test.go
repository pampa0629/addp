package config

import (
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

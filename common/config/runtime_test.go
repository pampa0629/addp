package config

import "testing"

func TestBuildServiceURLNormalizesPort(t *testing.T) {
	if got := BuildServiceURL("localhost", ":8185"); got != "http://localhost:8185" {
		t.Fatalf("BuildServiceURL() = %q, want %q", got, "http://localhost:8185")
	}
}

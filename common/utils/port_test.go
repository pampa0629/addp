package utils

import "testing"

func TestGetModulePortUsesDevelopStandardPort(t *testing.T) {
	t.Setenv("DEVELOP_BACKEND_PORT", "")

	if got := GetModulePort("develop"); got != "8185" {
		t.Fatalf("expected develop default port 8185, got %s", got)
	}
}

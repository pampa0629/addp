package config

import (
	"testing"
	"time"
)

func TestLoadGatewayRoutingDefaults(t *testing.T) {
	t.Setenv("SYSTEM_URL", "")
	t.Setenv("MODULE_REFRESH_INTERVAL", "")

	cfg := Load()
	if cfg.SystemServiceURL != "http://localhost:8180" {
		t.Fatalf("system service URL = %q", cfg.SystemServiceURL)
	}
	if cfg.ModuleRefreshInterval != 30*time.Second {
		t.Fatalf("module refresh interval = %s", cfg.ModuleRefreshInterval)
	}
}

package config

import (
	"testing"
	"time"
)

func TestLoadGatewayRoutingDefaults(t *testing.T) {
	t.Setenv("SYSTEM_URL", "")
	t.Setenv("MODULE_WATCH_TIMEOUT", "")

	cfg := Load()
	if cfg.SystemServiceURL != "http://localhost:8180" {
		t.Fatalf("system service URL = %q", cfg.SystemServiceURL)
	}
	if cfg.ModuleWatchTimeout != 10*time.Second {
		t.Fatalf("module watch timeout = %s", cfg.ModuleWatchTimeout)
	}
}

func TestLoadGatewayRejectsWatchTimeoutOutsideSystemContract(t *testing.T) {
	t.Setenv("MODULE_WATCH_TIMEOUT", "45s")
	if got := Load().ModuleWatchTimeout; got != 10*time.Second {
		t.Fatalf("module watch timeout = %s, want safe default", got)
	}
}

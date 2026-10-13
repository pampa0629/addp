package config

import (
	"testing"
	"time"
)

func TestLoadDefaultsServerAddrToDevelopStandardPort(t *testing.T) {
	t.Setenv("DEVELOP_BACKEND_PORT", "")

	cfg := Load()
	if cfg.ServerAddr != ":8185" {
		t.Fatalf("expected develop server addr :8185, got %s", cfg.ServerAddr)
	}
}

func TestLoadDefaultsDevelopServiceURLToStandardPort(t *testing.T) {
	t.Setenv("DEVELOP_URL", "")

	cfg := Load()
	if cfg.DevelopServiceURL != "http://localhost:8185" {
		t.Fatalf("expected develop service URL http://localhost:8185, got %s", cfg.DevelopServiceURL)
	}
}

func TestLoadQuerySupervisorLeaseDefaults(t *testing.T) {
	t.Setenv("DEVELOP_EXECUTION_LEASE_SECONDS", "")
	t.Setenv("DEVELOP_EXECUTION_HEARTBEAT_SECONDS", "")
	t.Setenv("DEVELOP_EXECUTION_CLAIM_INTERVAL_SECONDS", "")
	cfg := Load()
	if cfg.ExecutionLeaseDuration != 120*time.Second || cfg.ExecutionHeartbeatInterval != 30*time.Second || cfg.ExecutionClaimInterval != time.Second {
		t.Fatalf("unexpected lease config: %#v", cfg)
	}
}

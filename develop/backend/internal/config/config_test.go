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

func TestLoadQueryPolicyDefaults(t *testing.T) {
	cfg := Load()
	if cfg.DefaultQueryTimeout != 30 || cfg.MaxQueryTimeout != 300 || cfg.QueryResultLimit != 500 {
		t.Fatalf("unexpected query policy defaults: %#v", cfg)
	}
}

func TestLoadQuerySupervisorDefaultsToFixedLimits(t *testing.T) {
	t.Setenv("DEVELOP_QUERY_CONCURRENCY", "")
	t.Setenv("DEVELOP_QUERY_PER_ENGINE_CONCURRENCY", "")
	t.Setenv("DEVELOP_QUERY_WORKER_CONCURRENCY", "9")
	t.Setenv("DEVELOP_QUERY_LEASE_SECONDS", "")
	t.Setenv("DEVELOP_QUERY_HEARTBEAT_SECONDS", "")
	t.Setenv("DEVELOP_QUERY_CLAIM_INTERVAL_SECONDS", "")
	t.Setenv("DEVELOP_QUERY_IDLE_MAX_INTERVAL_SECONDS", "")
	cfg := Load()
	if cfg.QueryConcurrency != 20 || cfg.QueryPerEngineConcurrency != 5 || cfg.QueryLeaseDuration != 120*time.Second ||
		cfg.QueryHeartbeatInterval != 30*time.Second || cfg.QueryClaimInterval != time.Second ||
		cfg.QueryIdleMaxInterval != 30*time.Second {
		t.Fatalf("unexpected query supervisor defaults: %#v", cfg)
	}
}

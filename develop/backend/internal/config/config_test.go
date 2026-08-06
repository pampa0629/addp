package config

import "testing"

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

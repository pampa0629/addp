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

func TestLoadQueryResultLimit(t *testing.T) {
	t.Setenv("QUERY_RESULT_LIMIT", "750")
	if got := Load().QueryResultLimit; got != 750 {
		t.Fatalf("QueryResultLimit = %d, want 750", got)
	}

	t.Setenv("QUERY_RESULT_LIMIT", "0")
	if got := Load().QueryResultLimit; got != 500 {
		t.Fatalf("QueryResultLimit = %d, want default 500", got)
	}
}

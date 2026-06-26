package config

import "testing"

func TestLoadDefaultsCopilotServiceURLToStandardPort(t *testing.T) {
	t.Setenv("COPILOT_URL", "")
	t.Setenv("ENABLE_SERVICE_INTEGRATION", "false")

	cfg := Load()
	if cfg.CopilotServiceURL != "http://localhost:8087" {
		t.Fatalf("expected copilot service URL http://localhost:8087, got %s", cfg.CopilotServiceURL)
	}
}

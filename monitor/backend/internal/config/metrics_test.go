package config

import "testing"

func TestOptionalMetricsConfigurationDoesNotBlockBusinessConfig(t *testing.T) {
	for _, flag := range []string{"", "false", "true", "invalid"} {
		t.Run(flag, func(t *testing.T) {
			t.Setenv("ADDP_OBSERVABILITY_METRICS_ENABLED", flag)
			t.Setenv("MONITOR_METRICS_ALLOWED_CIDRS", "")
			t.Setenv("MONITOR_METRICS_ALLOWED_PORTS", "")
			cfg, err := LoadConfig()
			if err != nil || cfg == nil {
				t.Fatalf("optional metrics blocked config: %v", err)
			}
			if cfg.MetricsPolicy != nil || cfg.MetricsEnabled != (flag != "" && flag != "false") {
				t.Fatal("invalid metrics implicitly configured")
			}
		})
	}
}

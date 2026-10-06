package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
			if cfg.MetricsQueryClient != nil || cfg.MetricsPolicy != nil || cfg.MetricsEnabled != (flag != "" && flag != "false") {
				t.Fatal("invalid metrics implicitly configured")
			}
		})
	}
}

func TestMetricsQueryDoesNotBorrowAdmissionOrHealthFiles(t *testing.T) {
	t.Setenv("MONITOR_PROMETHEUS_URL", "https://localhost:9090")
	for _, key := range []string{"MONITOR_PROMETHEUS_CA_FILE", "MONITOR_PROMETHEUS_CLIENT_CERT_FILE", "MONITOR_PROMETHEUS_CLIENT_KEY_FILE"} {
		t.Setenv(key, "")
	}
	if loadMetricsQueryClient(true) != nil || loadMetricsQueryClient(false) != nil {
		t.Fatal("missing independent credentials implicitly configured")
	}
}

func TestMetricsCertificateRejectsNonRegularAndOversizedInputs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CERTIFICATE_FIXTURE", dir)
	if readMetricsCertificate("CERTIFICATE_FIXTURE") != nil {
		t.Fatal("directory accepted as a certificate")
	}
	file := filepath.Join(dir, "oversized")
	if e := os.WriteFile(file, make([]byte, (1<<20)+1), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("CERTIFICATE_FIXTURE", file)
	if readMetricsCertificate("CERTIFICATE_FIXTURE") != nil {
		t.Fatal("oversized certificate read")
	}
}

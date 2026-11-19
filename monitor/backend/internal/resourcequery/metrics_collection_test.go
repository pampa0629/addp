package resourcequery

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"
)

func TestIntegrationMetricsCollectionHealthSource(t *testing.T) {
	origin := os.Getenv("MONITOR_PROMETHEUS_URL")
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" || origin == "" {
		t.Skip("requires the owned live metrics fixture")
	}
	cert, err := tls.LoadX509KeyPair(os.Getenv("MONITOR_PROMETHEUS_CLIENT_CERT_FILE"), os.Getenv("MONITOR_PROMETHEUS_CLIENT_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile(os.Getenv("MONITOR_PROMETHEUS_CA_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	prometheus, err := NewClient(origin, ca, cert)
	if err != nil {
		t.Fatal(err)
	}
	defer prometheus.Close()
	health, err := prometheus.CollectionHealth(context.Background(), time.Now().UTC().Truncate(time.Second), DefaultBudget())
	if err != nil || health.Discovered != 1 || health.Online == nil || *health.Online != 1 ||
		health.Failed == nil || *health.Failed != 0 || health.RetentionBytes != 10*1024*1024*1024 || health.TSDBBytes <= 0 {
		t.Fatalf("unexpected collection health: %+v, %v", health, err)
	}
}

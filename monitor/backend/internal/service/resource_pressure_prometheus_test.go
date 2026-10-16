package service

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"

	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
)

// Real fixed-catalog samples use the same kernel as the background evaluator.
// The existing T2 probe owns the source, outage and mTLS center lifecycle.
func TestIntegrationMetricsResourcePressureEvidence(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	read := func(name string) []byte {
		t.Helper()
		b, e := os.ReadFile(os.Getenv(name))
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	cert, e := tls.X509KeyPair(read("MONITOR_PROMETHEUS_CLIENT_CERT_FILE"), read("MONITOR_PROMETHEUS_CLIENT_KEY_FILE"))
	if e != nil {
		t.Fatal(e)
	}
	backend, e := resourcequery.NewClient(os.Getenv("MONITOR_PROMETHEUS_URL"), read("MONITOR_PROMETHEUS_CA_FILE"), cert)
	if e != nil {
		t.Fatal(e)
	}
	defer backend.Close()
	scope := resourcequery.Scope{NodeID: os.Getenv("ADDP_METRICS_QUERY_NODE_ID"), Instance: os.Getenv("ADDP_METRICS_QUERY_INSTANCE")}
	down := os.Getenv("ADDP_RESOURCE_RULE_SOURCE_DOWN") == "1"
	deadline := time.Now().Add(45 * time.Second)
	for {
		at := time.Now().UTC().Truncate(time.Second)
		p, err := resourcequery.NewPlan([]string{"node.memory.used_percent", "node.uptime_seconds"}, at.Add(-315*time.Second), at, at, true, nil, resourcequery.DefaultBudget())
		if err != nil || p.StepSeconds != 15 || p.Points != 22 {
			t.Fatal(p, err)
		}
		rows, err := backend.Query(context.Background(), p, scope, resourcequery.DefaultBudget())
		if err != nil {
			t.Fatal(err)
		}
		var metric, uptime resourcequery.Series
		for _, row := range rows {
			if row.MetricKey == "node.memory.used_percent" {
				metric = row
			}
			if row.MetricKey == "node.uptime_seconds" {
				uptime = row
			}
		}
		policy := models.ResourcePressurePolicy{TriggerPercent: 100, RecoveryPercent: 99.99, TriggerSeconds: 30, RecoverySeconds: 30}
		normal := evaluateResourcePressure(metric, uptime, at.Add(-time.Hour), at, true, false, policy)
		if down {
			if normal.Verdict != PlatformAlertUnknown {
				t.Fatal("outage produced a normal or risk verdict", normal)
			}
			if evaluateResourcePressure(metric, uptime, at.Add(-time.Hour), at, true, true, policy).Verdict != PlatformAlertUnknown {
				t.Fatal("outage recovered active alert")
			}
			return
		}
		if normal.Verdict == PlatformAlertNormal {
			if normal.Minimum <= 0 || normal.Maximum >= 100 {
				t.Fatal("controlled native memory fixture must have headroom", normal)
			}
			policy.TriggerPercent = normal.Minimum / 2
			policy.RecoveryPercent = normal.Minimum / 4
			opened := evaluateResourcePressure(metric, uptime, at.Add(-time.Hour), at, true, false, policy)
			if opened.Verdict != PlatformAlertRisk || opened.Samples < 3 || opened.SampledTo.Sub(opened.SampledFrom) < 30*time.Second {
				t.Fatal("real window did not open", opened)
			}
			policy.TriggerPercent = 100
			policy.RecoveryPercent = (normal.Maximum + 100) / 2
			if evaluateResourcePressure(metric, uptime, at.Add(-time.Hour), at, true, true, policy).Verdict != PlatformAlertNormal {
				t.Fatal("real recovery window rejected")
			}
			if evaluateResourcePressure(metric, uptime, at, at, true, true, policy).Verdict != PlatformAlertUnknown {
				t.Fatal("fresh validity boundary recovered early")
			}
			if evaluateResourcePressure(metric, uptime, at.Add(-time.Hour), at, false, true, policy).Verdict != PlatformAlertPaused {
				t.Fatal("disabled source recovered")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no complete real pressure window: %+v", normal)
		}
		time.Sleep(time.Second)
	}
}

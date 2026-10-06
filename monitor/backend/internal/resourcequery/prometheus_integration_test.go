package resourcequery

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"
)

// Invoked only by the owned Prometheus T2 gate, while its native node source is active.
func TestIntegrationMetricsResourceQueries(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	read := func(name string) []byte {
		value, e := os.ReadFile(os.Getenv(name))
		if e != nil {
			t.Fatal(e)
		}
		return value
	}
	cert, e := tls.X509KeyPair(read("MONITOR_PROMETHEUS_CLIENT_CERT_FILE"), read("MONITOR_PROMETHEUS_CLIENT_KEY_FILE"))
	if e != nil {
		t.Fatal(e)
	}
	c, e := NewClient(os.Getenv("MONITOR_PROMETHEUS_URL"), read("MONITOR_PROMETHEUS_CA_FILE"), cert)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	keys := []string{}
	for _, d := range Catalog() {
		keys = append(keys, d.Key)
	}
	now := time.Now().UTC().Truncate(time.Second)
	b := DefaultBudget()
	scope := Scope{NodeID: testNode, Instance: os.Getenv("ADDP_METRICS_QUERY_INSTANCE")}
	for _, trend := range []bool{false, true} {
		p, e := NewPlan(keys, now.Add(-30*time.Second), now, now, trend, b)
		if e != nil {
			t.Fatal(e)
		}
		rows, e := c.Query(context.Background(), p, scope, b)
		if e != nil || len(rows) != len(keys) {
			t.Fatalf("native query trend=%v rows=%d error=%v", trend, len(rows), e)
		}
		for _, row := range rows {
			point := row.Points[len(row.Points)-1]
			if point.DataState != "valid" || point.SampledAt == nil || point.Value == nil {
				t.Fatalf("native evidence metric=%s trend=%v point=%+v", row.MetricKey, trend, point)
			}
		}
	}
	p, e := NewPlan(keys, now, now, now, false, b)
	if e != nil {
		t.Fatal(e)
	}
	scope.Instance = "127.0.0.1:1"
	rows, e := c.Query(context.Background(), p, scope, b)
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range rows {
		if row.Points[0].DataState != "no_data" || row.Points[0].Value != nil {
			t.Fatal("old or other source leaked into current endpoint query")
		}
	}
}

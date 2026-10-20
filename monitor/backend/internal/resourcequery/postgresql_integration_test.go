package resourcequery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

// Only the standard owned metrics gate supplies this current, synthetic PG15
// source. System/User adapters are covered separately, not represented as T4.
func TestIntegrationMetricsPostgreSQLSource(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	read := func(key string) []byte {
		value, err := os.ReadFile(os.Getenv(key))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	certificate, err := tls.X509KeyPair(read("MONITOR_PROMETHEUS_CLIENT_CERT_FILE"), read("MONITOR_PROMETHEUS_CLIENT_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(os.Getenv("MONITOR_PROMETHEUS_URL"), read("MONITOR_PROMETHEUS_CA_FILE"), certificate)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var record struct {
		DeploymentID string                         `json:"deployment_id"`
		Source       models.PostgreSQLMetricsSource `json:"source"`
		Instance     string                         `json:"instance"`
	}
	if err = json.Unmarshal(read("ADDP_POSTGRES_QUERY_RECORD"), &record); err != nil {
		t.Fatal(err)
	}
	scope := PostgreSQLScope{DeploymentID: record.DeploymentID, Source: record.Source, Instance: record.Instance}
	if scope.Validate() != nil {
		t.Fatal("invalid owned source record")
	}
	mode := os.Getenv("ADDP_POSTGRES_QUERY_EXPECT")
	keys := []string{}
	for _, d := range PostgreSQLCatalog() {
		keys = append(keys, d.Key)
	}
	budget := DefaultBudget()
	// HTTP SD refresh (30s), scrape scheduling (15s), SQL deadline (5s),
	// one complete counter window (60s), plus a 20s scheduling margin.
	started := time.Now()
	deadline := started.Add(130 * time.Second)
	firstSnapshot := false
	var now time.Time
	var result Summary
	for {
		now = time.Now().UTC().Truncate(time.Second)
		p, e := NewPostgreSQLPlan(keys, now, now, now, false, budget)
		if e != nil {
			t.Fatal(e)
		}
		result, err = c.PostgreSQLQuery(context.Background(), p, scope, budget)
		if err != nil {
			t.Fatal(err)
		}
		ready := result.Collection.State == "collecting"
		gauges := 0
		rates := 0
		for _, series := range result.Series {
			if series.Points[0].DataState == "valid" {
				if series.WindowSeconds == 0 {
					gauges++
				} else {
					rates++
				}
			}
		}
		if ready && gauges == 9 && !firstSnapshot {
			firstSnapshot = true
			t.Logf("First complete native snapshot after %.1fs", time.Since(started).Seconds())
		}
		switch mode {
		case "healthy":
			ready = ready && gauges == 9 && rates == 3
		case "reset":
			ready = ready && gauges == 9 && rates == 0
		case "sql-failed":
			ready = result.Collection.State == "snapshot_unavailable"
			if ready {
				for _, series := range result.Series {
					if series.Points[0].Value != nil {
						t.Fatal("HTTP 200/up=1 exposed an old SQL snapshot")
					}
				}
			}
		case "recovered":
			ready = ready && gauges == 9
		default:
			t.Fatal("unknown gate mode")
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			logPostgreSQLWindowEvidence(t, c, scope, now, budget)
			raw, _ := json.Marshal(result)
			t.Fatalf("PostgreSQL %s readiness: %s", mode, raw)
		}
		time.Sleep(time.Second)
	}
	if mode != "healthy" {
		return
	}
	if len(result.Series) != 12 {
		t.Fatal("catalog incomplete")
	}
	for _, series := range result.Series {
		if series.Points[0].Value == nil || *series.Points[0].Value < 0 {
			t.Fatal("missing fixed metric")
		}
		if series.MetricKey == "postgresql.database.total_bytes" && *series.Points[0].Value <= 0 {
			t.Fatal("capacity missing")
		}
	}
	summary, err := c.PostgreSQLSummaries(context.Background(), []PostgreSQLScope{scope}, now, budget)
	if err != nil || len(summary) != 1 || len(summary[scope.DeploymentID].Series) != 4 || summary[scope.DeploymentID].Collection.State != "collecting" {
		t.Fatalf("real summary: %+v %v", summary, err)
	}
	p, err := NewPostgreSQLPlan([]string{"postgresql.connections.total", "postgresql.transactions.committed_per_second"}, now.Add(-5*time.Minute), now, now, true, budget)
	if err != nil {
		t.Fatal(err)
	}
	p = LimitTrendStart(p, *scope.Source.StartedAt)
	trend, err := c.PostgreSQLQuery(context.Background(), p, scope, budget)
	if err != nil || len(trend.Series) != 2 {
		t.Fatal("real trend", err)
	}
	valid := 0
	for _, series := range trend.Series {
		for _, point := range series.Points {
			if point.EvaluatedAt.Before(*scope.Source.StartedAt) {
				t.Fatal("trend crossed container startup")
			}
			if point.DataState == "valid" {
				valid++
			}
		}
	}
	if valid == 0 {
		t.Fatal("real trend had no native samples")
	}
	foreign := scope
	foreign.Source.ContainerID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	instant, _ := NewPostgreSQLSummaryPlan(1, now, budget)
	for _, wrong := range []PostgreSQLScope{foreign, func() PostgreSQLScope { value := scope; value.DeploymentID = uuid.NewString(); return value }()} {
		row, err := c.PostgreSQLQuery(context.Background(), instant, wrong, budget)
		if err != nil || row.Collection.State != "no_sample" {
			t.Fatal("foreign current identity reused", err)
		}
		for _, series := range row.Series {
			if series.Points[0].Value != nil {
				t.Fatal("foreign identity returned a metric")
			}
		}
	}
}

func logPostgreSQLWindowEvidence(t *testing.T, c *Client, scope PostgreSQLScope, at time.Time, budget Budget) {
	t.Helper()
	raw := scope.selector("transactions_committed_total")
	for key, expression := range map[string]string{
		"window_samples":           `count_over_time(` + raw + `[1m])`,
		"window_resets":            `resets(` + raw + `[1m])`,
		"previous_count":           `count(` + raw + ` offset 1m) or vector(0)`,
		"now_snapshot_valid":       `(vector(1)` + scope.snapshotGuard("") + `) or vector(0)`,
		"previous_snapshot_valid":  `(vector(1)` + scope.snapshotGuard(" offset 1m") + `) or vector(0)`,
		"statistics_epoch_matches": `count(` + scope.selector("statistics_info") + ` and ` + scope.selector("statistics_info") + ` offset 1m) or vector(0)`,
	} {
		p, _ := NewPostgreSQLSummaryPlan(1, at, budget)
		_, query := planExpressionQuery(p, expression, budget)
		result, err := c.readForm(context.Background(), "/api/v1/query", query)
		if err != nil {
			t.Logf("PostgreSQL readiness signal %s unavailable", key)
			continue
		}
		values := []string{}
		for _, row := range result.Data.Result {
			if row.Value != nil {
				values = append(values, fmt.Sprint(string(row.Value[1])))
			}
		}
		t.Logf("PostgreSQL readiness signal %s=%v", key, values)
	}
}

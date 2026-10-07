package resourcequery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	b := DefaultBudget()
	scope := Scope{NodeID: testNode, Instance: os.Getenv("ADDP_METRICS_QUERY_INSTANCE")}
	// Gauges can be valid after one scrape; counters require a whole minute.
	deadline := time.Now().Add(100 * time.Second)
	var now time.Time
	for {
		now = time.Now().UTC().Truncate(time.Second)
		p, err := NewPlan(keys, now, now, now, false, b)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := c.Query(context.Background(), p, scope, b)
		if err != nil {
			t.Fatal(err)
		}
		ready := len(rows) == len(keys)
		for _, row := range rows {
			ready = ready && row.Points[0].DataState == "valid"
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("native catalog full-window readiness: %+v", rows)
		}
		time.Sleep(time.Second)
	}
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

// This uses the same pinned Prometheus image and the owned T2 lifecycle. It
// evaluates the actual catalog expression, including paired timestamp evidence.
func TestIntegrationMetricsCPUWindow(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	scope := Scope{NodeID: testNode, Instance: "fixture:9100"}
	p, err := NewPlan([]string{"node.cpu.busy_percent"}, testTime, testTime, testTime, false, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	expression, err := p.Expression(scope)
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.TrimSuffix(scope.selector("node_cpu_seconds_total"), "}") + `,mode="idle",cpu="%s"}`
	boot := scope.selector("node_boot_time_seconds")
	series := func(name, values string) map[string]any { return map[string]any{"series": name, "values": values} }
	type scenario struct {
		name, first, second, boot, at string
		value                         *float64
		extra                         string
	}
	n := func(v float64) *float64 { return &v }
	scenarios := []scenario{
		{"half busy", "100+7.5x8", "100+7.5x8", "1+0x8", "60s", n(50), ""},
		{"zero busy is valid", "100+15x8", "100+15x8", "1+0x8", "60s", n(0), ""},
		{"fully busy", "100+0x8", "100+0x8", "1+0x8", "60s", n(100), ""},
		{"insufficient startup", "100+7.5x8", "100+7.5x8", "1+0x8", "45s", nil, ""},
		{"missing inner sample", "100 107.5 _ 122.5 130 137.5 145 152.5 160", "100+7.5x8", "1+0x8", "60s", nil, ""},
		{"counter reset", "100 107.5 1 8.5 16 23.5 31 38.5 46", "100+7.5x8", "1+0x8", "60s", nil, ""},
		{"recovered after reset", "100 107.5 1 8.5 16 23.5 31 38.5 46", "100+7.5x8", "1+0x8", "90s", n(50), ""},
		{"core added", "100+7.5x8", "_ _ 100 107.5 115 122.5 130 137.5 145", "1+0x8", "60s", nil, ""},
		{"core removed", "100+7.5x8", "100 107.5 stale _ _ _ _ _ _", "1+0x8", "60s", nil, ""},
		{"host reboot", "100+7.5x8", "100+7.5x8", "1 1 30 30 30 30 30 30 30", "60s", nil, ""},
		{"recovered after reboot", "100+7.5x8", "100+7.5x8", "1 1 30 30 30 30 30 30 30", "90s", n(50), ""},
		{"missing latest core", "100+7.5x8", "100 107.5 115 122.5 _ 137.5 145 152.5 160", "1+0x8", "60s", nil, ""},
		{"impossible idle rate", "100+30x8", "100+7.5x8", "1+0x8", "60s", nil, ""},
		{"transient core", "100+7.5x8", "100+7.5x8", "1+0x8", "60s", nil, "_ _ 100 107.5 stale _ _ _ _"},
		{"recovered after transient core", "100+7.5x8", "100+7.5x8", "1+0x8", "120s", n(50), "_ _ 100 107.5 stale _ _ _ _"},
	}
	tests := []any{}
	for _, item := range scenarios {
		expected := []any{}
		if item.value != nil {
			at, err := time.ParseDuration(item.at)
			if err != nil {
				t.Fatal(err)
			}
			expected = append(expected,
				map[string]any{"labels": `{addp_component="value",addp_metric="node.cpu.busy_percent"}`, "value": *item.value},
				map[string]any{"labels": `{addp_component="sampled_at",addp_metric="node.cpu.busy_percent"}`, "value": at.Seconds()})
		}
		inputs := []any{series(fmt.Sprintf(raw, "0"), item.first), series(fmt.Sprintf(raw, "1"), item.second), series(boot, item.boot)}
		if item.extra != "" {
			inputs = append(inputs, series(fmt.Sprintf(raw, "2"), item.extra))
		}
		tests = append(tests, map[string]any{"name": item.name, "interval": "15s",
			"input_series":     inputs,
			"promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": item.at, "exp_samples": expected}}})
	}
	payload, err := json.Marshal(map[string]any{"rule_files": []any{}, "evaluation_interval": "15s", "tests": tests})
	if err != nil {
		t.Fatal(err)
	}
	work := os.Getenv("METRICS_T2_WORK")
	path := filepath.Join(work, "source", "cpu-query-tests.yml")
	if err = os.WriteFile(path, payload, 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "--env-file", "/dev/null", "-p", os.Getenv("METRICS_T2_PROJECT"), "-f", os.Getenv("METRICS_T2_COMPOSE"), "run", "--rm", "--no-deps", "--volume", filepath.Join(work, "source")+":/cpu-tests:ro", "--entrypoint", "/bin/promtool", "prometheus", "test", "rules", "/cpu-tests/cpu-query-tests.yml")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native CPU window scenarios: %v\n%s", err, output)
	}
	t.Logf("pinned promtool passed %d CPU window/reset/topology scenarios", len(scenarios))
}

package resourcequery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProcessBatchUsesTwoRequestsAndRejectsForeignEvidence(t *testing.T) {
	calls := 0
	foreign := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.RawQuery != "" || r.ParseForm() != nil {
			t.Error("unbounded query transport")
		}
		expression := r.PostForm.Get("query")
		for _, identity := range []string{`addp_instance_id="native-1"`, `addp_instance_id="native-2"`} {
			if !strings.Contains(expression, identity) {
				t.Error("instance scope lost")
			}
		}
		rows := []map[string]any{}
		for _, id := range []string{"1", "2"} {
			if strings.Contains(expression, "up_count") {
				fields := map[string]string{"up_count": "1", "up": "1", "sampled_at": fmt.Sprint(testTime.Unix()), "identity_valid": "1"}
				if id == "2" {
					fields["identity_valid"] = "0"
				}
				for _, row := range collectionWire(t, fields).Data.Result {
					row.Metric["addp_summary_process"] = id
					rows = append(rows, map[string]any{"metric": row.Metric, "value": row.Value})
				}
			} else {
				for _, metric := range ProcessCatalog() {
					for _, component := range []string{"value", "sampled_at"} {
						value := 0.
						if metric.Unit == "cores" {
							value = 1.5
						}
						if component == "sampled_at" {
							value = float64(testTime.Unix())
						}
						label := id
						if foreign {
							label = "3"
						}
						rows = append(rows, map[string]any{"metric": map[string]string{"addp_summary_process": label, "addp_metric": metric.Key, "addp_component": component}, "value": []any{testTime.Unix(), fmt.Sprint(value)}})
					}
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": rows}})
	}))
	defer server.Close()
	c := &Client{origin: server.URL, http: server.Client()}
	scopes := []ProcessScope{}
	for id := uint(1); id <= 2; id++ {
		scopes = append(scopes, ProcessScope{ID: id, ModuleName: "monitor", InstanceID: fmt.Sprint("native-", id), Role: "worker", Instance: fmt.Sprint("fixture:", 9440+id), StartedAt: time.Unix(1, 0)})
	}
	data, err := c.ProcessSummaries(context.Background(), scopes, testTime, DefaultBudget())
	if err != nil || calls != 2 || len(data) != 2 || data[1].Collection.State != "collecting" || *data[1].Series[0].Points[0].Value != 1.5 || *data[1].Series[1].Points[0].Value != 0 || data[2].Collection.State != "identity_mismatch" || data[2].Series[0].Points[0].Value != nil {
		t.Fatal("cross-process or zero/cores evidence", data, err, calls)
	}
	foreign = true
	if _, err = c.ProcessSummaries(context.Background(), scopes, testTime, DefaultBudget()); err == nil {
		t.Fatal("foreign process evidence accepted")
	}
}

func TestProcessCatalogAndBudgetStayDistinctFromHosts(t *testing.T) {
	b := DefaultBudget()
	if _, err := NewProcessSummaryPlan(34, testTime, b); err != ErrBudget {
		t.Fatal("unbounded batch", err)
	}
	if _, err := NewPlan([]string{"process.memory.resident_bytes"}, testTime, testTime, testTime, false, nil, b); err != ErrInvalid {
		t.Fatal("process metric entered host catalog", err)
	}
	scope := ProcessScope{ID: 1, ModuleName: "monitor", InstanceID: "worker", Role: "worker", Instance: "127.0.0.1:9445", StartedAt: time.Unix(1, 0)}
	p, err := NewProcessSummaryPlan(20, testTime, b)
	if err != nil {
		t.Fatal(err)
	}
	expression, err := scope.expression(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"process_cpu_seconds_total", "process_resident_memory_bytes", "process_start_time_seconds", "addp_process_identity_info", `addp_monitor_kind="process_resources"`, "offset 1m", "resets("} {
		if !strings.Contains(expression, required) {
			t.Fatal("missing ownership/window evidence", required)
		}
	}
}

func TestIntegrationMetricsProcessWindow(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	scope := ProcessScope{ID: 1, ModuleName: "monitor", InstanceID: "native-worker", Role: "worker", Instance: "fixture:9445", StartedAt: time.Unix(1, 0)}
	p, err := NewProcessSummaryPlan(1, testTime, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	expression, err := scope.expression(p)
	if err != nil {
		t.Fatal(err)
	}
	series := func(name, values string) map[string]any { return map[string]any{"series": name, "values": values} }
	number := func(n float64) *float64 { return &n }
	scenarios := []struct {
		name, cpu, identity, start, up, at string
		value                              *float64
		gauges                             bool
		extra                              string
	}{
		{"multi-core", "100+22.5x8", "1+0x8", "1+0x8", "1+0x8", "60s", number(1.5), true, ""},
		{"idle is zero", "100+0x8", "1+0x8", "1+0x8", "1+0x8", "60s", number(0), true, ""},
		{"startup retains gauges", "100+22.5x8", "1+0x8", "1+0x8", "1+0x8", "45s", nil, true, ""},
		{"missing inner sample", "100 122.5 _ 167.5 190 212.5 235 257.5 280", "1+0x8", "1+0x8", "1+0x8", "60s", nil, true, ""},
		{"missing latest CPU sample", "100 122.5 145 167.5 _ 212.5 235 257.5 280", "1+0x8", "1+0x8", "1+0x8", "60s", nil, true, ""},
		{"counter reset", "100 122.5 1 23.5 46 68.5 91 113.5 136", "1+0x8", "1+0x8", "1+0x8", "60s", nil, true, ""},
		{"recovered after reset", "100 122.5 1 23.5 46 68.5 91 113.5 136", "1+0x8", "1+0x8", "1+0x8", "90s", number(1.5), true, ""},
		{"different process start", "100+22.5x8", "1+0x8", "2+0x8", "1+0x8", "60s", nil, false, ""},
		{"restart at same endpoint", "100+22.5x8", "1+0x8", "1 1 30 30 30 30 30 30 30", "1+0x8", "60s", nil, false, ""},
		{"identity value invalid", "100+22.5x8", "0+0x8", "1+0x8", "1+0x8", "60s", nil, false, ""},
		{"up down", "100+22.5x8", "1+0x8", "1+0x8", "0+0x8", "60s", nil, false, ""},
		{"identity sample not current", "100+22.5x8", "1 1 1 1 _ 1 1 1 1", "1+0x8", "1+0x8", "60s", nil, false, ""},
		{"another self identity", "100+22.5x8", "1+0x8", "1+0x8", "1+0x8", "60s", nil, false, "foreign"},
	}
	tests := []any{}
	for _, item := range scenarios {
		elapsed, _ := time.ParseDuration(item.at)
		expected := []any{}
		add := func(key string, value float64) {
			expected = append(expected, map[string]any{"labels": fmt.Sprintf(`{addp_component="value",addp_metric="%s"}`, key), "value": value}, map[string]any{"labels": fmt.Sprintf(`{addp_component="sampled_at",addp_metric="%s"}`, key), "value": elapsed.Seconds()})
		}
		if item.value != nil {
			add("process.cpu.core_equivalents", *item.value)
		}
		if item.gauges {
			add("process.memory.resident_bytes", 4096)
			add("process.uptime_seconds", elapsed.Seconds()-1)
		}
		identity := strings.ReplaceAll(scope.identitySelector(), `operating_system=~"darwin|linux"`, `operating_system="linux"`)
		inputs := []any{series(scope.selector("process_cpu_seconds_total"), item.cpu), series(identity, item.identity), series(scope.selector("process_start_time_seconds"), item.start), series(scope.selector("process_resident_memory_bytes"), "4096+0x8"), series(scope.selector("up"), item.up)}
		if item.extra != "" {
			inputs = append(inputs, series(strings.Replace(identity, `runtime_instance_id="native-worker"`, `runtime_instance_id="foreign"`, 1), "1+0x8"))
		}
		tests = append(tests, map[string]any{"name": item.name, "interval": "15s", "input_series": inputs, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": item.at, "exp_samples": expected}}})
	}
	runPromtoolCases(t, "process", tests, true)
	t.Logf("pinned promtool passed %d process ownership/window scenarios", len(scenarios))
}

package resourcequery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIntegrationMetricsHTTPWindow(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	scope := httpScope()
	scope.StartedAt = time.Unix(1, 0)
	series := func(name, values string) any { return map[string]any{"series": name, "values": values} }
	identity := strings.ReplaceAll(scope.identitySelector(), `operating_system=~"darwin|linux"`, `operating_system="linux"`)
	tests := []any{}
	for _, name := range []string{"complete", "zero traffic", "missing scrape", "failed scrape", "failed boundary scrape", "missing boundary scrape", "warming up", "current concurrency", "first error status"} {
		grouped := name == "first error status"
		at := "120s"
		up := "1+0x8"
		count := "0+2x8"
		if name == "zero traffic" {
			count = "0+0x8"
		}
		if name == "missing scrape" {
			up = "1 1 1 1 1 1 1 _ 1"
		}
		if name == "failed scrape" {
			up = "1 1 1 1 1 1 0 1 1"
		}
		if name == "failed boundary scrape" {
			up = "1 1 1 1 0 1 1 1 1"
		}
		if name == "missing boundary scrape" {
			up = "1 1 1 1 _ 1 1 1 1"
			count = "0 2 4 6 _ 10 12 14 16"
		}
		if name == "warming up" || name == "current concurrency" {
			at = "45s"
		}
		p, err := NewHTTPPlan(testTime, testTime, testTime, false, grouped, nil, "", 60, DefaultBudget())
		if err != nil {
			t.Fatal(err)
		}
		expression, err := scope.httpExpression(p)
		if err != nil {
			t.Fatal(err)
		}
		metric := "http.requests.per_second"
		expected := []any{}
		inputs := []any{series(scope.selector("up"), up), series(identity, "1+0x8"), series(scope.selector("process_start_time_seconds"), "1+0x8"), series(scope.selector("addp_http_server_instrumentation_start_time_seconds"), "1+0x8"), series(scope.selector("addp_http_server_duration_seconds_count"), count), series(scope.selector("addp_http_server_failures_total"), "0+1x8"), series(scope.selector("addp_http_server_client_errors_total"), "0+0x8")}
		if name == "current concurrency" {
			metric = "http.active_requests"
			inputs = append(inputs, series(strings.TrimSuffix(scope.selector("http_server_active_requests"), "}")+`,http_request_method="GET",url_scheme="http"}`, "0+0x8"))
		}
		if grouped {
			metric = "http.failures.percent"
			raw := func(status, errorType string) string {
				return strings.TrimSuffix(scope.selector("http_server_request_duration_seconds_count"), "}") + `,http_route="/items/:id",http_request_method="GET",http_response_status_code="` + status + `",error_type="` + errorType + `"}`
			}
			inputs = append(inputs, series(raw("200", ""), "0+1x8"), series(raw("500", "500"), "_ _ _ _ _ _ _ 1 1"))
		}
		expression = `(` + expression + `) and on(addp_metric) label_replace(vector(1),"addp_metric",` + strconv.Quote(metric) + `,"","")`
		if name == "complete" || name == "zero traffic" || name == "current concurrency" || grouped {
			value := 2.0 / 15
			if name == "zero traffic" || name == "current concurrency" {
				value = 0
			}
			labels := ""
			if grouped {
				value = 20
				labels = `,http_request_method="GET",http_route="/items/:id"`
			}
			sampledAt := 120.0
			if name == "current concurrency" {
				sampledAt = 45
			}
			for _, part := range []struct {
				key   string
				value float64
			}{{"value", value}, {"sampled_at", sampledAt}} {
				expected = append(expected, map[string]any{"labels": fmt.Sprintf(`{addp_component="%s",addp_metric="%s"%s}`, part.key, metric, labels), "value": part.value})
			}
		}
		tests = append(tests, map[string]any{"name": name, "interval": "15s", "input_series": inputs, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": at, "exp_samples": expected}}})
	}
	// Both rankings must retain one shared eligible top ten across all metrics.
	for _, window := range []int{300, 900} {
		p, _ := NewHTTPPlan(testTime, testTime, testTime, false, false, nil, "", window, DefaultBudget())
		expression, err := scope.httpExpression(p)
		if err != nil {
			t.Fatal(err)
		}
		expression = `(` + expression + `) and on(addp_metric) label_replace(vector(1),"addp_metric","http.requests.per_second","","")`
		inputs := []any{series(scope.selector("up"), "1+0x120"), series(identity, "1+0x120"), series(scope.selector("process_start_time_seconds"), "1+0x120"), series(scope.selector("addp_http_server_instrumentation_start_time_seconds"), "1+0x120"), series(scope.selector("addp_http_server_duration_seconds_count"), "0+2x120")}
		expected := []any{map[string]any{"labels": `{addp_component="value",addp_metric="http.requests.per_second"}`, "value": 2.0 / 15}, map[string]any{"labels": `{addp_component="sampled_at",addp_metric="http.requests.per_second"}`, "value": 1800}}
		tests = append(tests, map[string]any{"name": fmt.Sprintf("complete %d second window", window), "interval": "15s", "input_series": inputs, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "1800s", "exp_samples": expected}}})
	}
	for _, rank := range []string{"mean", "qps"} {
		p, _ := NewHTTPPlan(testTime, testTime, testTime, false, true, nil, "", 60, DefaultBudget())
		p.Rank = rank
		expression, err := scope.httpExpression(p)
		if err != nil {
			t.Fatal(err)
		}
		expression = `(` + expression + `) and on(addp_metric,addp_component) label_replace(label_replace(vector(1),"addp_metric","http.requests.per_second","",""),"addp_component","value","","")`
		inputs := []any{series(scope.selector("up"), "1+0x8"), series(identity, "1+0x8"), series(scope.selector("process_start_time_seconds"), "1+0x8"), series(scope.selector("addp_http_server_instrumentation_start_time_seconds"), "1+0x8"), series(scope.selector("addp_http_server_duration_seconds_count"), "0+210x8")}
		expected := []any{}
		for i := 1; i <= 20; i++ {
			route := fmt.Sprintf("/template-%02d/:id", i)
			labels := `,http_route=` + strconv.Quote(route) + `,http_request_method="GET",http_response_status_code="200",error_type=""}`
			inputs = append(inputs, series(strings.TrimSuffix(scope.selector("http_server_request_duration_seconds_count"), "}")+labels, fmt.Sprintf("0+%dx8", i)), series(strings.TrimSuffix(scope.selector("http_server_request_duration_seconds_sum"), "}")+labels, fmt.Sprintf("0+%dx8", i*i)))
			if i > 10 {
				expected = append(expected, map[string]any{"labels": `{addp_component="value",addp_metric="http.requests.per_second",http_request_method="GET",http_route=` + strconv.Quote(route) + `}`, "value": float64(i) / 15})
			}
		}
		tests = append(tests, map[string]any{"name": "bounded ranking " + rank, "interval": "15s", "input_series": inputs, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "120s", "exp_samples": expected}}})
	}
	runPromtoolCases(t, "http", tests, true)
}

func TestIntegrationMetricsHTTPRealSource(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" || os.Getenv("ADDP_PROCESS_T2_RECORDS") == "" {
		t.Skip("requires standard metrics T2 HTTP source")
	}
	read := func(path string) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	cert, err := tls.X509KeyPair(read(os.Getenv("MONITOR_PROMETHEUS_CLIENT_CERT_FILE")), read(os.Getenv("MONITOR_PROMETHEUS_CLIENT_KEY_FILE")))
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(os.Getenv("MONITOR_PROMETHEUS_URL"), read(os.Getenv("MONITOR_PROMETHEUS_CA_FILE")), cert)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var records []struct {
		ID         string    `json:"id"`
		Module     string    `json:"module_name"`
		InstanceID string    `json:"instance_id"`
		Role       string    `json:"role"`
		Started    time.Time `json:"started_at"`
		Address    string    `json:"instance"`
	}
	if err := json.Unmarshal(read(os.Getenv("ADDP_PROCESS_T2_RECORDS")), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Role != "backend" {
		t.Fatal("missing owned backend HTTP source")
	}
	r := records[0]
	id, _ := strconv.ParseUint(r.ID, 10, 63)
	scope := ProcessScope{ID: uint(id), ModuleName: r.Module, InstanceID: r.InstanceID, Role: r.Role, StartedAt: r.Started, Instance: r.Address}
	if os.Getenv("ADDP_PROCESS_T2_STOPPED") == "1" {
		at := time.Now().UTC().Truncate(time.Second)
		p, _ := NewHTTPPlan(at, at, at, false, false, nil, "", 60, DefaultBudget())
		rows, err := client.HTTP(context.Background(), p, scope, DefaultBudget())
		if err != nil || len(rows) != 1 {
			t.Fatal("stopped HTTP source", err)
		}
		for _, line := range rows[0].Series {
			if line.Points[0].Value != nil {
				t.Fatal("stopped HTTP source retained value")
			}
		}
		return
	}
	deadline := time.Now().Add(100 * time.Second)
	for {
		at := time.Now().UTC().Truncate(time.Second)
		p, _ := NewHTTPPlan(at, at, at, false, false, nil, "", 60, DefaultBudget())
		rows, err := client.HTTP(context.Background(), p, scope, DefaultBudget())
		ready := err == nil && len(rows) == 1
		if ready {
			for _, line := range rows[0].Series {
				ready = ready && line.Points[0].DataState == "valid" && line.Points[0].Value != nil
			}
		}
		if ready {
			for _, line := range rows[0].Series {
				value := *line.Points[0].Value
				if line.MetricKey == "http.failures.percent" && (value < 10 || value > 40) {
					t.Fatal("native failure ratio", value)
				}
				if line.MetricKey == "http.client_errors.percent" && (value < 10 || value > 40) {
					t.Fatal("native 404 ratio", value)
				}
			}
			grouped, _ := NewHTTPPlan(at, at, at, false, true, nil, "", 60, DefaultBudget())
			interfaces, err := client.HTTP(context.Background(), grouped, scope, DefaultBudget())
			if err != nil || len(interfaces) != 4 {
				t.Fatal("native templates", interfaces, err)
			}
			for _, row := range interfaces {
				if strings.Contains(row.Route, "fixture-id") {
					t.Fatal("raw request path entered templates")
				}
			}
			foreign := scope
			foreign.InstanceID = "foreign-previous"
			other, err := client.HTTP(context.Background(), p, foreign, DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range other[0].Series {
				if line.Points[0].Value != nil {
					t.Fatal("HTTP source identity leak")
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("native HTTP window unavailable", rows, err)
		}
		time.Sleep(time.Second)
	}
}

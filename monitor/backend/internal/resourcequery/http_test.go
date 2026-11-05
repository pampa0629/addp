package resourcequery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPHistoryUsesOneStoredAddressAndPreservesWindowGaps(t *testing.T) {
	scope := httpScope()
	scope.Instance = ""
	scope.StartedAt = time.Unix(1, 0)
	p, err := NewHTTPPlan(testTime.Add(-15*time.Second), testTime, testTime, true, false, nil, "", 60, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	candidate := func(signal, addr, value string) string {
		return fmt.Sprintf(`{"metric":{"instance":%q,"addp_history_signal":%q},"value":%s}`, addr, signal, pair(testTime.Unix(), value))
	}
	address := "127.0.0.1:9443"
	prefix := candidate("identity", address, "1") + "," + candidate("min", address, "1") + ","
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.URL.Path == "/api/v1/query" {
			query := r.URL.Query().Get("query")
			if !strings.Contains(query, `addp_instance_id="ingress-one"`) || strings.Contains(query, address) {
				t.Error("historical lookup relied on a current source address")
			}
			evidence := prefix + candidate("max", address, "1")
			switch call {
			case 3:
				evidence = prefix + candidate("max", address, "2")
			case 4:
				evidence = prefix + candidate("max", "127.0.0.2:9443", "1")
			case 5:
				evidence = ""
			}
			fmt.Fprint(w, data(evidence, false))
			return
		}
		if err := r.ParseForm(); err != nil || r.URL.Path != "/api/v1/query_range" ||
			!strings.Contains(r.Form.Get("query"), `instance="127.0.0.1:9443"`) ||
			!strings.Contains(r.Form.Get("query"), `offset 60s))>=1799999940.000000000`) {
			t.Error("range query did not use the verified source", err)
		}
		row := func(component, before, at string) string {
			return fmt.Sprintf(`{"metric":{"addp_metric":"http.requests.per_second","addp_component":%q},"values":[%s,%s]}`,
				component, pair(p.Start.Unix(), before), pair(p.End.Unix(), at))
		}
		fmt.Fprint(w, data(row("value", "1", "0")+","+row("sampled_at", "1799999985", "1800000000"), true))
	}))
	defer server.Close()
	client := &Client{origin: server.URL, http: server.Client()}
	query := func() ([]HTTPObservation, error) {
		return client.HTTPHistory(context.Background(), p, scope, testTime.Add(-60*time.Second), DefaultBudget())
	}
	rows, err := query()
	if err != nil || len(rows) != 1 || calls.Load() != 2 {
		t.Fatal(rows, err, calls.Load())
	}
	points := rows[0].Series[0].Points
	if points[0].DataState != "no_data" || points[0].Value != nil || points[1].Value == nil || *points[1].Value != 0 {
		t.Fatal("out-of-bounds or real-zero point misclassified", points)
	}
	if _, err = query(); !errors.Is(err, ErrUnavailable) || calls.Load() != 3 {
		t.Fatal("reused address and foreign startup reached range query", err, calls.Load())
	}
	if _, err = query(); !errors.Is(err, ErrUnavailable) || calls.Load() != 4 {
		t.Fatal("multiple addresses reached range query", err, calls.Load())
	}
	if rows, err = query(); err != nil || len(rows) != 0 || calls.Load() != 5 {
		t.Fatal("absent history was inferred", rows, err, calls.Load())
	}
}

func httpScope() ProcessScope {
	return ProcessScope{ID: 1, ModuleName: "gateway", InstanceID: "ingress-one", Role: "ingress", Instance: "fixture:9443", StartedAt: time.Unix(0, 0)}
}

func TestHTTPPlansHaveClosedWindowsAndNeverMixRuntimeLayers(t *testing.T) {
	for _, window := range []int{60, 300, 900} {
		p, err := NewHTTPPlan(testTime, testTime, testTime, false, false, nil, "", window, DefaultBudget())
		if err != nil || len(p.Metrics) != 7 {
			t.Fatal(p, err)
		}
		expression, err := httpScope().httpExpression(p)
		if err != nil || !strings.Contains(expression, `addp_runtime_role="ingress"`) || strings.Contains(expression, `addp_runtime_role=~`) || !strings.Contains(expression, "min_over_time(") || !strings.Contains(expression, "instrumentation_start_time") {
			t.Fatal(expression, err)
		}
		if strings.Contains(expression, " or vector(0)") {
			t.Fatal("missing HTTP samples must not become healthy zero values")
		}
	}
	for _, window := range []int{0, 30, 61, 3600} {
		if _, err := NewHTTPPlan(testTime, testTime, testTime, false, false, nil, "", window, DefaultBudget()); !errors.Is(err, ErrInvalid) {
			t.Fatal(window, err)
		}
	}
	for _, role := range []string{"worker", "scheduler"} {
		p, _ := NewHTTPPlan(testTime, testTime, testTime, false, false, nil, "", 60, DefaultBudget())
		scope := httpScope()
		scope.Role = role
		if _, err := scope.httpExpression(p); !errors.Is(err, ErrInvalid) {
			t.Fatal(role, err)
		}
	}
	route := "/items/:id"
	p, err := NewHTTPPlan(testTime.Add(-time.Hour), testTime, testTime, true, false, &route, "GET", 60, DefaultBudget())
	if err != nil || len(p.Metrics) != 6 || p.Points*6 > DefaultBudget().MaxTotalPoints {
		t.Fatal(p, err)
	}
	expression, err := httpScope().httpExpression(p)
	if err != nil || !strings.Contains(expression, `http_route="/items/:id",http_request_method="GET"`) || !strings.Contains(expression, `-on() (sum by()`) {
		t.Fatal(expression, err)
	}
	if strings.Contains(expression, "http_server_active_requests") {
		t.Fatal("process active requests must not be presented as route concurrency")
	}
}

func TestHTTPNormalizerRejectsForeignLabelsDuplicatePointsAndBudgetOverflow(t *testing.T) {
	p, err := NewHTTPPlan(testTime, testTime, testTime, false, true, nil, "", 60, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	row := func(component, value string) string {
		return `{"metric":{"http_route":"/items/:id","http_request_method":"GET","addp_metric":"http.requests.per_second","addp_component":"` + component + `"},"value":` + pair(testTime.Unix(), value) + `}`
	}
	valid := row("value", "0") + "," + row("sampled_at", "1800000000")
	result, err := normalizeHTTP(wire(t, data(valid, false)), p, DefaultBudget())
	if err != nil || len(result) != 1 || result[0].Route != "/items/:id" || result[0].Series[0].Points[0].Value == nil || *result[0].Series[0].Points[0].Value != 0 {
		t.Fatal(result, err)
	}
	if result[0].Series[1].Points[0].Value != nil {
		t.Fatal("missing failure rate must remain unknown")
	}
	unmatched := strings.ReplaceAll(valid, `"http_route":"/items/:id",`, "")
	result, err = normalizeHTTP(wire(t, data(unmatched, false)), p, DefaultBudget())
	if err != nil || len(result) != 1 || result[0].Route != "" {
		t.Fatal("empty unmatched route omitted by Prometheus", result, err)
	}
	for _, body := range []string{valid + "," + row("value", "1"), strings.ReplaceAll(valid, `"GET"`, `"PRIVATE"`), strings.ReplaceAll(valid, `"addp_metric"`, `"instance":"private","addp_metric"`)} {
		if _, err := normalizeHTTP(wire(t, data(body, false)), p, DefaultBudget()); err == nil {
			t.Fatal("accepted foreign, duplicate or unbounded labels")
		}
	}
	b := DefaultBudget()
	b.MaxSeries = 5
	if _, err := normalizeHTTP(wire(t, data(valid, false)), p, b); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
}

func TestHTTPNormalizerKeepsStoppedTrafficZeroDenominatorUnknown(t *testing.T) {
	p, _ := NewHTTPPlan(testTime, testTime, testTime, false, false, nil, "", 60, DefaultBudget())
	row := func(metric, component, value string) string {
		return `{"metric":{"addp_metric":"` + metric + `","addp_component":"` + component + `"},"value":` + pair(testTime.Unix(), value) + `}`
	}
	body := row("http.requests.per_second", "value", "0") + "," + row("http.requests.per_second", "sampled_at", "1800000000") + "," +
		row("http.failures.percent", "value", "NaN") + "," + row("http.failures.percent", "sampled_at", "1800000000")
	rows, err := normalizeHTTP(wire(t, data(body, false)), p, DefaultBudget())
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	for _, line := range rows[0].Series {
		if line.MetricKey == "http.requests.per_second" && (line.Points[0].Value == nil || *line.Points[0].Value != 0) {
			t.Fatal("idle QPS lost")
		}
		if line.MetricKey == "http.failures.percent" && line.Points[0].Value != nil {
			t.Fatal("0/0 became a healthy failure rate")
		}
	}
}

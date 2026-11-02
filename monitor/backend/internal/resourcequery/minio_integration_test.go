package resourcequery

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Fixed samples exercise the real Prometheus evaluator, independently of the
// owned native source proof. This does not claim a live authorized HTTP SD chain.
func TestIntegrationMinIOMetricQueries(t *testing.T) {
	if os.Getenv("ADDP_MINIO_OBSERVATION_T2") != "1" {
		t.Skip("requires owned MinIO source gate")
	}
	project := os.Getenv("T2_MINIO_PROJECT")
	if !regexp.MustCompile(`^addp-minio-observation-t2-[0-9a-f]{12}$`).MatchString(project) {
		t.Fatal("invalid owned MinIO project")
	}
	scope := queryEngineScope()
	scope.EngineType = "minio"
	started := time.Unix(0, 0).UTC()
	scope.Source.StartedAt = &started
	type input struct {
		Series string `json:"series"`
		Values string `json:"values"`
	}
	epoch := strings.Repeat("a", 64)
	counters := []string{"requests_total", "errors_5xx_total", "sent_bytes_total", "received_bytes_total"}
	raw := func(field, generation string) string {
		selector := scope.selector(field)
		if generation != "" {
			selector = strings.TrimSuffix(selector, "}") + `,counter_epoch=` + strconv.Quote(generation) + `}`
		}
		return selector
	}
	baseline := func() []input {
		rows := []input{{scope.selector("up"), "1x8"}, {raw("observed_at_seconds", ""), "0+15x8"}, {raw("start_time_seconds", ""), "1x8"}, {raw("uptime_seconds", ""), "0 14+15x7"}}
		for field, value := range map[string]string{"drives_online": "1x8", "capacity_total_bytes": "100x8", "capacity_free_bytes": "25x8", "scan_object_bytes": "40x8", "scan_objects": "2x8", "scan_buckets": "1x8", "scan_time_seconds": "30x8", "requests_total": "0+30x8", "errors_5xx_total": "0+3x8", "sent_bytes_total": "0+150x8", "received_bytes_total": "0+300x8"} {
			gen := ""
			for _, c := range counters {
				if c == field {
					gen = epoch
				}
			}
			rows = append(rows, input{raw(field, gen), value})
		}
		return rows
	}
	factor := 60.0 / 45.0
	factor /= 60.0
	ptr := func(v float64) *float64 { return &v }
	type scenario struct {
		name, key, change string
		value             *float64
	}
	cases := []scenario{}
	values := []*float64{ptr(119), ptr(1), nil, ptr(100), ptr(25), ptr(75), ptr(40), ptr(2), ptr(1), ptr(90), ptr(90 * factor), nil, nil, ptr(9 * factor), ptr(100 * (9 * factor) / (90 * factor)), ptr(450 * factor), ptr(900 * factor)}
	for i, d := range MinIOCatalog() {
		cases = append(cases, scenario{d.Key, d.Key, "", values[i]})
	}
	for _, item := range []scenario{
		{"omitted_optional_keeps_uptime", "minio.uptime_seconds", "missing-free", ptr(119)},
		{"missing_free", "minio.capacity.used_percent", "missing-free", nil},
		{"zero_total", "minio.capacity.used_percent", "zero-total", nil},
		{"free_exceeds_total", "minio.capacity.used_percent", "large-free", nil},
		{"published_zero", "minio.drives.offline", "zero-offline", ptr(0)},
		{"no_scan", "minio.scan.object_bytes", "no-scan", nil},
		{"future_scan", "minio.scan.age_seconds", "future-scan", nil},
		{"scan_predates_container", "minio.scan.age_seconds", "historic-scan", ptr(90)},
		{"counter_reset", "minio.requests.per_second", "reset", nil},
		{"epoch_changed", "minio.requests.per_second", "epoch", nil},
		{"missing_boundary", "minio.requests.per_second", "boundary", nil},
		{"incomplete_window", "minio.requests.per_second", "samples", nil},
		{"ratio_zero_denominator", "minio.requests.error_5xx_percent", "no-requests", nil},
		{"omitted_error_is_not_zero", "minio.requests.error_5xx_percent", "missing-5xx", nil},
		{"ratio_epoch_mismatch", "minio.requests.error_5xx_percent", "ratio-epoch", nil},
		{"ratio_exceeds_total", "minio.requests.error_5xx_percent", "large-5xx", nil},
		{"source_down", "minio.capacity.total_bytes", "down", nil},
		{"missing_uptime", "minio.capacity.total_bytes", "missing-uptime", nil},
		{"old_observation", "minio.capacity.total_bytes", "old-observation", nil},
		{"foreign_identity", "minio.capacity.total_bytes", "identity", nil},
		{"duplicate_optional", "minio.drives.online", "duplicate", nil},
		{"duplicate_optional_is_local", "minio.uptime_seconds", "duplicate", ptr(119)},
		{"missing_epoch", "minio.requests.per_second", "no-epoch", nil},
	} {
		cases = append(cases, item)
	}
	tests := []any{}
	for _, tc := range cases {
		rows := baseline()
		current := scope
		for i := range rows {
			r := &rows[i]
			switch {
			case tc.change == "missing-free" && r.Series == raw("capacity_free_bytes", ""):
				r.Values = "25x7 stale"
			case tc.change == "zero-total" && r.Series == raw("capacity_total_bytes", ""):
				r.Values = "0x8"
			case tc.change == "large-free" && r.Series == raw("capacity_free_bytes", ""):
				r.Values = "101x8"
			case tc.change == "no-scan" && r.Series == raw("scan_time_seconds", ""):
				r.Values = "30x7 stale"
			case tc.change == "future-scan" && r.Series == raw("scan_time_seconds", ""):
				r.Values = "121x8"
			case tc.change == "reset" && r.Series == raw("requests_total", epoch):
				r.Values = "0+30x6 0+30x1"
			case tc.change == "epoch" && r.Series == raw("requests_total", epoch):
				r.Values = "0+30x4 stale _x2"
			case tc.change == "boundary" && r.Series == raw("requests_total", epoch):
				r.Values = "0+30x3 stale 150+30x3"
			case tc.change == "samples" && r.Series == raw("requests_total", epoch):
				r.Values = "0+30x5 _ 210+30x1"
			case tc.change == "no-requests" && r.Series == raw("requests_total", epoch):
				r.Values = "1x8"
			case tc.change == "missing-5xx" && r.Series == raw("errors_5xx_total", epoch):
				r.Values = "0+3x7 stale"
			case tc.change == "ratio-epoch" && r.Series == raw("errors_5xx_total", epoch):
				r.Series = raw("errors_5xx_total", strings.Repeat("b", 64))
			case tc.change == "large-5xx" && r.Series == raw("errors_5xx_total", epoch):
				r.Values = "0+60x8"
			case tc.change == "down" && r.Series == scope.selector("up"):
				r.Values = "1x7 0"
			case tc.change == "missing-uptime" && r.Series == raw("uptime_seconds", ""):
				r.Values = "0 14+15x6 stale"
			case tc.change == "old-observation" && r.Series == raw("observed_at_seconds", ""):
				r.Values = "0x8"
			case tc.change == "no-epoch" && r.Series == raw("requests_total", epoch):
				r.Series = raw("requests_total", "")
			}
			if tc.change == "historic-scan" {
				if r.Series == raw("start_time_seconds", "") {
					r.Values = "61x8"
				}
				if r.Series == raw("uptime_seconds", "") {
					r.Values = "0x7 59"
				}
			}
		}
		switch tc.change {
		case "zero-offline":
			rows = append(rows, input{raw("drives_offline", ""), "0x8"})
		case "epoch":
			rows = append(rows, input{raw("requests_total", strings.Repeat("b", 64)), "_x4 0+30x3"})
		case "identity":
			current.Instance = "127.0.0.2:9443"
		case "historic-scan":
			at := time.Unix(60, 0).UTC()
			current.Source.StartedAt = &at
			for i := range rows {
				rows[i].Series = strings.ReplaceAll(rows[i].Series, started.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano))
			}
		case "duplicate":
			rows = append(rows, input{strings.TrimSuffix(raw("drives_online", ""), "}") + `,extra="duplicate"}`, "1x8"})
		}
		at := time.Unix(120, 0).UTC()
		plan, err := NewEnginePlan("minio", []string{tc.key}, at, at, at, false, DefaultBudget())
		if err != nil {
			t.Fatal(err)
		}
		expression, err := current.expression(plan)
		if err != nil {
			t.Fatal(err)
		}
		samples := []any{}
		if tc.value != nil {
			for _, component := range []string{"value", "sampled_at"} {
				v := *tc.value
				if component == "sampled_at" {
					v = 120
				}
				samples = append(samples, map[string]any{"labels": `{addp_metric=` + strconv.Quote(tc.key) + `,addp_component=` + strconv.Quote(component) + `}`, "value": v})
			}
		}
		tests = append(tests, map[string]any{"name": tc.name, "interval": "15s", "input_series": rows, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "2m", "exp_samples": samples}}})
	}
	document, err := json.Marshal(map[string]any{"evaluation_interval": "15s", "tests": tests})
	if err != nil {
		t.Fatal(err)
	}
	runEnginePromQLTests(t, document, project, "minio-observation", "minio-observation")
	t.Logf("MinIO fixed PromQL: %d cases passed; owned promtool removed", len(cases))
}

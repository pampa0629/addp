package resourcequery

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationMetricsDiskIOWindow(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	scope := Scope{NodeID: testNode, Instance: "fixture:9100"}
	type scenario struct {
		name, key, numerator, denominator, at string
		valid                                 bool
		value                                 float64
		extra, wrongDevice                    bool
	}
	busy, read, write := "node.disk.io_busy_percent", "node.disk.read_mean_duration_milliseconds", "node.disk.write_mean_duration_milliseconds"
	cases := []scenario{
		{name: "busy half", key: busy, numerator: "100+7.5x8", at: "60s", valid: true, value: 50},
		{name: "busy full", key: busy, numerator: "100+15x8", at: "60s", valid: true, value: 100},
		{name: "idle busy", key: busy, numerator: "100+0x8", at: "60s", valid: true},
		{name: "busy out of range", key: busy, numerator: "100+30x8", at: "60s"},
		{name: "busy reset", key: busy, numerator: "100 107.5 1 8.5 16 23.5 31 38.5 46", at: "60s"},
		{name: "read duration", key: read, numerator: "100+3x8", denominator: "100+1500x8", at: "60s", valid: true, value: 2},
		{name: "write duration", key: write, numerator: "100+7.5x8", denominator: "100+1500x8", at: "60s", valid: true, value: 5},
		{name: "zero duration with requests", key: read, numerator: "100+0x8", denominator: "100+15x8", at: "60s", valid: true},
		{name: "no requests", key: read, numerator: "100+0x8", denominator: "100+0x8", at: "60s"},
		{name: "no requests with time increment", key: read, numerator: "100+3x8", denominator: "100+0x8", at: "60s"},
		{name: "startup window", key: read, numerator: "100+3x8", denominator: "100+15x8", at: "45s"},
		{name: "missing numerator", key: read, numerator: "100 103 _ 109 112 115 118 121 124", denominator: "100+15x8", at: "60s"},
		{name: "missing denominator", key: read, numerator: "100+3x8", denominator: "100 115 _ 145 160 175 190 205 220", at: "60s"},
		{name: "missing boundary denominator", key: read, numerator: "100+3x8", denominator: "_ 115 130 145 160 175 190 205 220", at: "60s"},
		{name: "reset numerator", key: read, numerator: "100 103 1 4 7 10 13 16 19", denominator: "100+15x8", at: "60s"},
		{name: "reset denominator", key: read, numerator: "100+3x8", denominator: "100 115 1 16 31 46 61 76 91", at: "60s"},
		{name: "boundary denominator reset", key: read, numerator: "100+3x8", denominator: "100 1 16 31 46 61 76 91 106", at: "60s"},
		{name: "negative denominator", key: read, numerator: "100+3x8", denominator: "-100+15x8", at: "60s"},
		{name: "duplicate denominator", key: read, numerator: "100+3x8", denominator: "100+15x8", at: "60s", extra: true},
		{name: "foreign device denominator", key: read, numerator: "100+3x8", denominator: "100+15x8", at: "60s", wrongDevice: true},
		{name: "duration reset recovery", key: read, numerator: "100+3x8", denominator: "100 1600 1 1501 3001 4501 6001 7501 9001", at: "90s", valid: true, value: 2},
	}
	tests := []any{}
	for _, item := range cases {
		p, err := NewPlan([]string{item.key}, testTime, testTime, testTime, false, Dimensions{"device": "sda"}, DefaultBudget())
		if err != nil {
			t.Fatal(err)
		}
		expr, err := p.Expression(scope)
		if err != nil {
			t.Fatal(err)
		}
		numerator, denominator := "node_disk_io_time_seconds_total", ""
		if item.key == read {
			numerator, denominator = "node_disk_read_time_seconds_total", "node_disk_reads_completed_total"
		}
		if item.key == write {
			numerator, denominator = "node_disk_write_time_seconds_total", "node_disk_writes_completed_total"
		}
		selector := func(name, device string) string {
			return strings.TrimSuffix(scope.selector(name), "}") + fmt.Sprintf(`,device=%q}`, device)
		}
		inputs := []any{map[string]any{"series": selector(numerator, "sda"), "values": item.numerator}, map[string]any{"series": scope.selector("node_boot_time_seconds"), "values": "1+0x8"}}
		if denominator != "" {
			device := "sda"
			if item.wrongDevice {
				device = "sdb"
			}
			inputs = append(inputs, map[string]any{"series": selector(denominator, device), "values": item.denominator})
			if item.extra {
				inputs = append(inputs, map[string]any{"series": strings.TrimSuffix(selector(denominator, device), "}") + `,variant="duplicate"}`, "values": item.denominator})
			}
		}
		at, _ := time.ParseDuration(item.at)
		expected := []any{map[string]any{"labels": fmt.Sprintf(`{addp_component="observed_at",addp_metric=%q,device="sda"}`, item.key), "value": at.Seconds()}}
		if item.valid {
			for _, component := range []string{"value", "sampled_at"} {
				v := item.value
				if component == "sampled_at" {
					v = at.Seconds()
				}
				expected = append(expected, map[string]any{"labels": fmt.Sprintf(`{addp_component=%q,addp_metric=%q,device="sda"}`, component, item.key), "value": v})
			}
		}
		tests = append(tests, map[string]any{"name": item.name, "interval": "15s", "input_series": inputs, "promql_expr_test": []any{map[string]any{"expr": expr, "eval_time": item.at, "exp_samples": expected}}})
	}
	runPromtoolCases(t, "disk-io", tests, true)
	t.Logf("pinned promtool passed %d IO timing scenarios", len(tests))
}

func TestDiskIOCatalogAndIdleNormalization(t *testing.T) {
	keys := []string{"node.disk.io_busy_percent", "node.disk.read_mean_duration_milliseconds", "node.disk.write_mean_duration_milliseconds"}
	p, err := NewPlan(keys, testTime, testTime, testTime, false, Dimensions{"device": "sda"}, DefaultBudget())
	if err != nil || p.SeriesUpperBound != 3 || len(Catalog()) != 25 {
		t.Fatal(p, err)
	}
	rows := []string{}
	for _, key := range keys {
		rows = append(rows, fmt.Sprintf(`{"metric":{"addp_metric":%q,"addp_component":"observed_at","device":"sda"},"value":[1800000000,"1800000000"]}`, key))
	}
	rows = append(rows, `{"metric":{"addp_metric":"node.disk.io_busy_percent","addp_component":"value","device":"sda"},"value":[1800000000,"0"]}`, `{"metric":{"addp_metric":"node.disk.io_busy_percent","addp_component":"sampled_at","device":"sda"},"value":[1800000000,"1800000000"]}`)
	data := wire(t, `{"status":"success","data":{"resultType":"vector","result":[`+strings.Join(rows, ",")+`]}}`)
	out, err := normalize(data, p, DefaultBudget())
	if err != nil || len(out) != 3 || out[0].Points[0].DataState != "valid" || out[1].Points[0].DataState != "no_data" || out[2].Points[0].DataState != "no_data" || out[1].Unit != "milliseconds" {
		t.Fatal(out, err)
	}
	(*data.Data.Result[3].Value)[1] = []byte(`"101"`)
	out, err = normalize(data, p, DefaultBudget())
	if err != nil || out[0].Points[0].DataState != "no_data" {
		t.Fatal("out of range busy accepted", out, err)
	}
}

package resourcequery

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDiskDimensionsBudgetAndNormalization(t *testing.T) {
	keys := []string{"node.disk.read_bytes_per_second", "node.disk.write_bytes_per_second"}
	p, err := NewPlan(keys, testTime, testTime, testTime, false, Dimensions{"device": "nvme0n1"}, DefaultBudget())
	if err != nil || p.DiskGroups != 1 || p.SeriesUpperBound != 2 {
		t.Fatal(p, err)
	}
	for _, dims := range []Dimensions{{"mountpoint": "/"}, {"device": ""}, {"device": "sda", "private": "secret"}, {"device": "sda", "mountpoint": "/", "fstype": "ext4"}} {
		if _, err := NewPlan(keys, testTime, testTime, testTime, false, dims, DefaultBudget()); !errors.Is(err, ErrInvalid) {
			t.Fatal("disk accepted invalid dimensions", dims, err)
		}
	}
	rows := []string{}
	for _, key := range keys {
		for _, component := range []string{"value", "sampled_at", "observed_at"} {
			v := float64(testTime.Unix())
			if component == "value" {
				v = 2048
			}
			rows = append(rows, fmt.Sprintf(`{"metric":{"addp_metric":%q,"addp_component":%q,"device":"nvme0n1"},"value":[%d,%q]}`, key, component, testTime.Unix(), fmt.Sprint(v)))
		}
	}
	body := `{"status":"success","data":{"resultType":"vector","result":[` + strings.Join(rows, ",") + `]}}`
	data := wire(t, body)
	out, err := normalize(data, p, DefaultBudget())
	if err != nil || len(out) != 2 || out[0].Points[0].DataState != "valid" || out[0].Dimensions["device"] != "nvme0n1" {
		t.Fatal(out, err)
	}

	mismatched := wire(t, body)
	(*mismatched.Data.Result[2].Value)[1] = []byte(`"1799999999"`)
	proof, e := normalize(mismatched, p, DefaultBudget())
	if e != nil || proof[0].Points[0].Value != nil || proof[0].Points[0].DataState != "no_data" {
		t.Fatal("disk witness mismatch accepted", proof, e)
	}
	for _, bad := range []string{strings.Replace(body, `"nvme0n1"`, `"other"`, 1), strings.Replace(body, `"device":"nvme0n1"`, `"device":"nvme0n1","private":"secret"`, 1)} {
		if _, err := normalize(wire(t, bad), p, DefaultBudget()); !errors.Is(err, ErrUnavailable) {
			t.Fatal("unscoped device accepted", err)
		}
	}

	mixedKeys := append(append([]string{}, keys...), "node.filesystem.total_bytes")
	mixed, err := NewPlan(mixedKeys, testTime, testTime, testTime, false, nil, DefaultBudget())
	if err != nil || mixed.DiskGroups != 33 || mixed.FilesystemGroups != 33 || mixed.SeriesUpperBound != 99 {
		t.Fatal("mixed dimension budget", mixed, err)
	}
	if _, err := NewPlan(mixedKeys, testTime, testTime, testTime, false, Dimensions{"device": "nvme0n1"}, DefaultBudget()); !errors.Is(err, ErrInvalid) {
		t.Fatal("mixed selected families accepted", err)
	}
	mixedEnvelope := wire(t, strings.Replace(body, `]}}`, `,{"metric":{"addp_metric":"node.filesystem.total_bytes","addp_component":"observed_at","device":"/dev/nvme0n1","mountpoint":"/","fstype":"ext4"},"value":[1800000000,"1800000000"]}]}}`, 1))
	grouped, err := normalize(mixedEnvelope, mixed, DefaultBudget())
	if err != nil || len(grouped) != 3 || len(grouped[0].Dimensions) != 1 || len(grouped[2].Dimensions) != 3 {
		t.Fatal("families polluted each other", grouped, err)
	}
	b := DefaultBudget()
	b.MaxSeries = 2
	broad, _ := NewPlan(keys, testTime, testTime, testTime, false, nil, b)
	data.Data.Result = append(data.Data.Result, data.Data.Result[0])
	data.Data.Result[len(data.Data.Result)-1].Metric = map[string]string{"addp_metric": keys[0], "addp_component": "observed_at", "device": "sdb"}
	if _, err := normalize(data, broad, b); !errors.Is(err, ErrBudget) {
		t.Fatal("device budget bypass", err)
	}
}

func TestIntegrationMetricsDiskWindow(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	scope := Scope{NodeID: testNode, Instance: "fixture:9100"}
	raw := strings.TrimSuffix(scope.selector("node_disk_read_bytes_total"), "}") + `,device="sda"}`
	boot := scope.selector("node_boot_time_seconds")
	p, err := NewPlan([]string{"node.disk.read_bytes_per_second"}, testTime, testTime, testTime, false, Dimensions{"device": "sda"}, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	expression, err := p.Expression(scope)
	if err != nil {
		t.Fatal(err)
	}
	type scenario struct {
		name, values, boot, at string
		valid                  bool
		value                  float64
		extra                  bool
	}
	cases := []scenario{
		{"read rate", "100+30720x8", "1+0x8", "60s", true, 2048, false},
		{"negative counter", "-100+30720x8", "1+0x8", "60s", false, 0, false},
		{"zero rate", "100+0x8", "1+0x8", "60s", true, 0, false},
		{"startup window", "100+30720x8", "1+0x8", "45s", false, 0, false},
		{"missing inner sample", "100 30820 _ 92260 122980 153700 184420 215140 245860", "1+0x8", "60s", false, 0, false},
		{"reset at window boundary", "100 1 30721 61441 92161 122881 153601 184321 215041", "1+0x8", "60s", false, 0, false},
		{"counter reset", "100 30820 1 30721 61441 92161 122881 153601 184321", "1+0x8", "60s", false, 0, false},
		{"reset recovery", "100 30820 1 30721 61441 92161 122881 153601 184321", "1+0x8", "90s", true, 2048, false},
		{"reboot", "100+30720x8", "1 1 30 30 30 30 30 30 30", "60s", false, 0, false},
		{"duplicate device", "100+30720x8", "1+0x8", "60s", false, 0, true},
	}
	tests := []any{}
	for _, item := range cases {
		at, _ := time.ParseDuration(item.at)
		expected := []any{map[string]any{"labels": `{addp_component="observed_at",addp_metric="node.disk.read_bytes_per_second",device="sda"}`, "value": at.Seconds()}}
		if item.valid {
			for _, component := range []string{"value", "sampled_at"} {
				v := item.value
				if component == "sampled_at" {
					v = at.Seconds()
				}
				expected = append(expected, map[string]any{"labels": fmt.Sprintf(`{addp_component=%q,addp_metric="node.disk.read_bytes_per_second",device="sda"}`, component), "value": v})
			}
		}
		inputs := []any{map[string]any{"series": raw, "values": item.values}, map[string]any{"series": boot, "values": item.boot}}
		if item.extra {
			inputs = append(inputs, map[string]any{"series": strings.TrimSuffix(raw, "}") + `,variant="duplicate"}`, "values": item.values})
		}
		if item.name == "read rate" {
			writePlan, e := NewPlan([]string{"node.disk.write_bytes_per_second"}, testTime, testTime, testTime, false, Dimensions{"device": "sda"}, DefaultBudget())
			if e != nil {
				t.Fatal(e)
			}
			writeExpression, e := writePlan.Expression(scope)
			if e != nil {
				t.Fatal(e)
			}
			writeExpected := []any{}
			for _, sample := range expected {
				copy := map[string]any{}
				for k, v := range sample.(map[string]any) {
					copy[k] = v
				}
				copy["labels"] = strings.Replace(copy["labels"].(string), "node.disk.read_bytes_per_second", "node.disk.write_bytes_per_second", 1)
				writeExpected = append(writeExpected, copy)
			}
			tests = append(tests, map[string]any{"name": "write rate", "interval": "15s", "input_series": []any{map[string]any{"series": strings.Replace(raw, "node_disk_read_bytes_total", "node_disk_written_bytes_total", 1), "values": item.values}, map[string]any{"series": boot, "values": item.boot}}, "promql_expr_test": []any{map[string]any{"expr": writeExpression, "eval_time": item.at, "exp_samples": writeExpected}}})
		}
		tests = append(tests, map[string]any{"name": item.name, "interval": "15s", "input_series": inputs, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": item.at, "exp_samples": expected}}})
	}
	// Prometheus rates have last-bit rounding; labels, absence and series counts remain exact.
	runPromtoolCases(t, "disk", tests, true)
	t.Logf("pinned promtool passed %d disk window/reset/device scenarios", len(tests))
}

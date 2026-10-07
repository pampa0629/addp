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

// Invoked only by the owned Prometheus T2 gate, for full and restricted sources.
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
		if !d.Filesystem() {
			keys = append(keys, d.Key)
		}
	}
	b := DefaultBudget()
	scope := Scope{NodeID: os.Getenv("ADDP_METRICS_QUERY_NODE_ID"), Instance: os.Getenv("ADDP_METRICS_QUERY_INSTANCE")}
	restricted := os.Getenv("ADDP_METRICS_QUERY_RESTRICTED_VM") == "1"
	// Gauges can be valid after one scrape; counters require a whole minute.
	deadline := time.Now().Add(100 * time.Second)
	var now time.Time
	for {
		now = time.Now().UTC().Truncate(time.Second)
		p, err := NewPlan(keys, now, now, now, false, nil, b)
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
		p, e := NewPlan(keys, now.Add(-30*time.Second), now, now, trend, nil, b)
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

	for _, inode := range []bool{false, true} {
		filesystemKeys := []string{}
		for _, d := range Catalog() {
			if d.Filesystem() && strings.HasPrefix(d.Key, "node.filesystem.inodes_") == inode {
				filesystemKeys = append(filesystemKeys, d.Key)
			}
		}
		files, err := NewPlan(filesystemKeys, now, now, now, false, nil, b)
		if err != nil {
			t.Fatal(err)
		}
		mounts, err := c.Query(context.Background(), files, scope, b)
		if err != nil || len(mounts) < len(filesystemKeys) {
			t.Fatal("native filesystem query", mounts, err)
		}
		if restricted {
			if len(mounts) != len(filesystemKeys) {
				t.Fatal("restricted source fabricated mount identities", mounts)
			}
			for _, row := range mounts {
				if len(row.Dimensions) != 0 || row.Points[0].DataState != "no_data" || row.Points[0].Value != nil {
					t.Fatal("restricted source leaked filesystem data", row)
				}
			}
			trend, err := NewPlan(filesystemKeys, now.Add(-30*time.Second), now, now, true, nil, b)
			if err != nil {
				t.Fatal(err)
			}
			history, err := c.Query(context.Background(), trend, scope, b)
			if err != nil || len(history) != len(filesystemKeys) {
				t.Fatal("restricted filesystem trend", err)
			}
			for _, row := range history {
				for _, point := range row.Points {
					if point.DataState != "no_data" || point.Value != nil || point.SampledAt != nil {
						t.Fatal("restricted source fabricated history", row)
					}
				}
			}
			continue
		}
		valid := map[string]int{}
		selectedDims := Dimensions{}
		for _, row := range mounts {
			if row.Dimensions.Validate() != nil || len(row.Dimensions) != 3 {
				t.Fatal("native mount identity", row)
			}
			if row.Points[0].DataState == "valid" {
				valid[row.Dimensions.identity()]++
				if valid[row.Dimensions.identity()] == len(filesystemKeys) {
					selectedDims = row.Dimensions
				}
			} else if !inode || row.Points[0].DataState != "no_data" {
				t.Fatal("native filesystem evidence", row)
			}
		}
		if len(selectedDims) != 3 {
			t.Fatal("native filesystem has no fully valid mount", inode)
		}
		selected, err := NewPlan(filesystemKeys, now.Add(-30*time.Second), now, now, true, selectedDims, b)
		if err != nil {
			t.Fatal(err)
		}
		history, err := c.Query(context.Background(), selected, scope, b)
		if err != nil || len(history) != len(filesystemKeys) {
			t.Fatal("native mount trend", err)
		}
		for _, row := range history {
			if row.Dimensions.identity() != selectedDims.identity() || row.Points[len(row.Points)-1].DataState != "valid" {
				t.Fatal(row)
			}
		}
	}
	p, e := NewPlan(keys, now, now, now, false, nil, b)
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
	p, err := NewPlan([]string{"node.cpu.busy_percent"}, testTime, testTime, testTime, false, nil, DefaultBudget())
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
	runPromtoolCases(t, "cpu", tests)
	t.Logf("pinned promtool passed %d CPU window/reset/topology scenarios", len(scenarios))
}

func runPromtoolCases(t *testing.T, name string, tests []any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"rule_files": []any{}, "evaluation_interval": "15s", "tests": tests})
	if err != nil {
		t.Fatal(err)
	}
	work := os.Getenv("METRICS_T2_WORK")
	filename := name + "-query-tests.yml"
	path := filepath.Join(work, "source", filename)
	if err = os.WriteFile(path, payload, 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "--env-file", "/dev/null", "-p", os.Getenv("METRICS_T2_PROJECT"), "-f", os.Getenv("METRICS_T2_COMPOSE"), "run", "--rm", "--no-deps", "--volume", filepath.Join(work, "source")+":/query-tests:ro", "--entrypoint", "/bin/promtool", "prometheus", "test", "rules", "/query-tests/"+filename)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native %s scenarios: %v\n%s", name, err, output)
	}
}

func TestIntegrationMetricsFilesystem(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	scope := Scope{NodeID: testNode, Instance: "fixture:9100"}
	keys := []string{"node.filesystem.total_bytes", "node.filesystem.free_bytes", "node.filesystem.available_bytes", "node.filesystem.used_bytes", "node.filesystem.used_percent"}
	p, err := NewPlan(keys, testTime, testTime, testTime, false, nil, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	expression, err := p.Expression(scope)
	if err != nil {
		t.Fatal(err)
	}
	dim := `,device="/dev/a",mountpoint="/",fstype="ext4"}`
	raw := func(name, labels string) string { return strings.TrimSuffix(scope.selector(name), "}") + labels }
	inputs := func(total, free, available, deviceError string) []any {
		out := []any{}
		for _, row := range [][2]string{{"node_filesystem_size_bytes", total}, {"node_filesystem_free_bytes", free}, {"node_filesystem_avail_bytes", available}, {"node_filesystem_device_error", deviceError}} {
			out = append(out, map[string]any{"series": raw(row[0], dim), "values": row[1]})
		}
		return out
	}
	expected := func(at float64, values []float64) []any {
		out := []any{}
		for i, key := range keys {
			components := []struct {
				k string
				v float64
			}{{"observed_at", at}}
			if values != nil {
				components = append(components, struct {
					k string
					v float64
				}{"value", values[i]}, struct {
					k string
					v float64
				}{"sampled_at", at})
			}
			for _, component := range components {
				out = append(out, map[string]any{"labels": fmt.Sprintf(`{addp_component=%q,addp_metric=%q,device="/dev/a",mountpoint="/",fstype="ext4"}`, component.k, key), "value": component.v})
			}
		}
		return out
	}
	type scenario struct {
		name, total, free, available, deviceError, at string
		values                                        []float64
	}
	percentage := func(used, capacity float64) float64 { return (used / capacity) * 100 }
	scenarios := []scenario{
		{"reserved non-root capacity", "100 100 100", "30 30 30", "25 25 25", "0 0 0", "30s", []float64{100, 30, 25, 70, percentage(70, 95)}},
		{"valid zero usage", "100 100 100", "100 100 100", "95 95 95", "0 0 0", "30s", []float64{100, 100, 95, 0, 0}},
		{"full capacity", "100 100 100", "0 0 0", "0 0 0", "0 0 0", "30s", []float64{100, 0, 0, 100, 100}},
		{"large full capacity stays exactly 100", "9007199254740016 9007199254740016 9007199254740016", "0 0 0", "0 0 0", "0 0 0", "30s", []float64{9007199254740016, 0, 0, 9007199254740016, 100}},
		{"invalid available", "100 100 100", "30 30 30", "31 31 31", "0 0 0", "30s", nil},
		{"invalid free", "100 100 100", "101 101 101", "25 25 25", "0 0 0", "30s", nil},
		{"negative capacity", "100 100 100", "30 30 30", "-1 -1 -1", "0 0 0", "30s", nil},
		{"nonfinite capacity", "+Inf +Inf +Inf", "30 30 30", "25 25 25", "0 0 0", "30s", nil},
		{"statfs error", "100 100 100", "30 30 30", "25 25 25", "1 1 1", "30s", nil},
		{"statfs capacity absent", "_ _ _", "_ _ _", "_ _ _", "1 1 1", "30s", nil},
		{"same scrape required", "100 100 100", "30 30 _", "25 25 25", "0 0 0", "30s", nil},
		{"historical capacity", "100 200 200", "30 130 130", "25 125 125", "0 0 0", "0s", []float64{100, 30, 25, 70, percentage(70, 95)}},
		{"changed capacity", "100 200 200", "30 130 130", "25 125 125", "0 0 0", "30s", []float64{200, 130, 125, 70, percentage(70, 195)}},
	}
	tests := []any{}
	for _, item := range scenarios {
		at, _ := time.ParseDuration(item.at)
		tests = append(tests, map[string]any{"name": item.name, "interval": "15s", "input_series": inputs(item.total, item.free, item.available, item.deviceError), "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": item.at, "exp_samples": expected(at.Seconds(), item.values)}}})
	}
	// Source duplication is rejected even after its private error label is dropped.
	duplicate := inputs("100 100 100", "30 30 30", "25 25 25", "0 0 0")
	duplicate = append(duplicate, map[string]any{"series": raw("node_filesystem_size_bytes", strings.TrimSuffix(dim, "}")+`,private="duplicate"}`), "values": "100 100 100"})
	tests = append(tests, map[string]any{"name": "duplicate source series", "interval": "15s", "input_series": duplicate, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "30s", "exp_samples": expected(30, nil)}}})

	bindInputs := inputs("100 100 100", "30 30 30", "25 25 25", "0 0 0")
	bindExpected := expected(30, []float64{100, 30, 25, 70, percentage(70, 95)})
	for _, raw := range inputs("100 100 100", "30 30 30", "25 25 25", "0 0 0") {
		row := raw.(map[string]any)
		row["series"] = strings.Replace(row["series"].(string), `mountpoint="/"`, `mountpoint="/bind"`, 1)
		bindInputs = append(bindInputs, row)
	}
	for _, raw := range expected(30, []float64{100, 30, 25, 70, percentage(70, 95)}) {
		row := raw.(map[string]any)
		row["labels"] = strings.Replace(row["labels"].(string), `mountpoint="/"`, `mountpoint="/bind"`, 1)
		bindExpected = append(bindExpected, row)
	}
	tests = append(tests, map[string]any{"name": "bind mounts are separate observations", "interval": "15s", "input_series": bindInputs, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "30s", "exp_samples": bindExpected}}})
	exact, err := NewPlan(keys, testTime, testTime, testTime, false, Dimensions{"device": "/dev/a", "mountpoint": "/", "fstype": "ext4"}, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	exactExpression, err := exact.Expression(scope)
	if err != nil {
		t.Fatal(err)
	}
	tests = append(tests, map[string]any{"name": "exact mount does not include bind sibling", "interval": "15s", "input_series": bindInputs, "promql_expr_test": []any{map[string]any{"expr": exactExpression, "eval_time": "30s", "exp_samples": expected(30, []float64{100, 30, 25, 70, percentage(70, 95)})}}})
	zeroExpected := expected(30, []float64{0, 0, 0, 0, 0})
	zeroRows := []any{}
	for _, raw := range zeroExpected {
		row := raw.(map[string]any)
		if !strings.Contains(row["labels"].(string), `addp_metric="node.filesystem.used_percent"`) || strings.Contains(row["labels"].(string), `addp_component="observed_at"`) {
			zeroRows = append(zeroRows, row)
		}
	}
	tests = append(tests, map[string]any{"name": "zero denominator is absent, zero capacity remains valid", "interval": "15s", "input_series": inputs("0 0 0", "0 0 0", "0 0 0", "0 0 0"), "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "30s", "exp_samples": zeroRows}}})
	tests = append(tests, map[string]any{"name": "unmounted source creates gaps", "interval": "15s", "input_series": inputs("100 stale _", "30 stale _", "25 stale _", "0 stale _"), "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "30s", "exp_samples": []any{}}}})
	runPromtoolCases(t, "filesystem", tests)
	t.Logf("pinned promtool passed %d filesystem capacity/evidence scenarios", len(tests))
}

func TestIntegrationMetricsFilesystemInodes(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	scope := Scope{NodeID: testNode, Instance: "fixture:9100"}
	keys := []string{"node.filesystem.inodes_total", "node.filesystem.inodes_free", "node.filesystem.inodes_used", "node.filesystem.inodes_used_percent"}
	p, err := NewPlan(keys, testTime, testTime, testTime, false, nil, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	expression, err := p.Expression(scope)
	if err != nil {
		t.Fatal(err)
	}
	dim := `,device="/dev/a",mountpoint="/",fstype="ext4"}`
	inputs := func(total, free, deviceError string) []any {
		out := []any{}
		for _, row := range [][2]string{{"node_filesystem_files", total}, {"node_filesystem_files_free", free}, {"node_filesystem_device_error", deviceError}} {
			out = append(out, map[string]any{"series": strings.TrimSuffix(scope.selector(row[0]), "}") + dim, "values": row[1]})
		}
		return out
	}
	expected := func(at float64, values []float64) []any {
		out := []any{}
		for i, key := range keys {
			labels := `{addp_metric="` + key + `",device="/dev/a",mountpoint="/",fstype="ext4",addp_component="`
			out = append(out, map[string]any{"labels": labels + `observed_at"}`, "value": at})
			if values != nil {
				out = append(out, map[string]any{"labels": labels + `value"}`, "value": values[i]}, map[string]any{"labels": labels + `sampled_at"}`, "value": at})
			}
		}
		return out
	}
	scenarios := []struct {
		name, total, free, deviceError, at string
		values                             []float64
	}{
		{"normal inode capacity", "100 100 100", "80 80 80", "0 0 0", "30s", []float64{100, 80, 20, 20}},
		{"valid empty", "100 100 100", "100 100 100", "0 0 0", "30s", []float64{100, 100, 0, 0}},
		{"full inode capacity", "100 100 100", "0 0 0", "0 0 0", "30s", []float64{100, 0, 100, 100}},
		{"large full inode stays exactly 100", "9007199254740016 9007199254740016 9007199254740016", "0 0 0", "0 0 0", "30s", []float64{9007199254740016, 0, 9007199254740016, 100}},
		{"zero total unsupported", "0 0 0", "0 0 0", "0 0 0", "30s", nil},
		{"free exceeds total", "100 100 100", "101 101 101", "0 0 0", "30s", nil},
		{"negative free", "100 100 100", "-1 -1 -1", "0 0 0", "30s", nil},
		{"fractional total", "100.5 100.5 100.5", "80 80 80", "0 0 0", "30s", nil},
		{"fractional free", "100 100 100", "80.5 80.5 80.5", "0 0 0", "30s", nil},
		{"integer range exceeded", "9007199254740992 9007199254740992 9007199254740992", "0 0 0", "0 0 0", "30s", nil},
		{"largest exact count", "9007199254740991 9007199254740991 9007199254740991", "9007199254740991 9007199254740991 9007199254740991", "0 0 0", "30s", []float64{9007199254740991, 9007199254740991, 0, 0}},
		{"nonfinite total", "+Inf +Inf +Inf", "80 80 80", "0 0 0", "30s", nil},
		{"nan free", "100 100 100", "NaN NaN NaN", "0 0 0", "30s", nil},
		{"missing free", "100 100 100", "_ _ _", "0 0 0", "30s", nil},
		{"same scrape required", "100 100 100", "80 80 _", "0 0 0", "30s", nil},
		{"statfs error", "100 100 100", "80 80 80", "1 1 1", "30s", nil},
		{"historical inode total", "100 200 200", "80 180 180", "0 0 0", "0s", []float64{100, 80, 20, 20}},
		{"changed inode total", "100 200 200", "80 180 180", "0 0 0", "30s", []float64{200, 180, 20, 10}},
	}
	tests := []any{}
	for _, item := range scenarios {
		at, _ := time.ParseDuration(item.at)
		tests = append(tests, map[string]any{"name": item.name, "interval": "15s", "input_series": inputs(item.total, item.free, item.deviceError), "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": item.at, "exp_samples": expected(at.Seconds(), item.values)}}})
	}
	duplicate := inputs("100 100 100", "80 80 80", "0 0 0")
	row := map[string]any{"series": strings.TrimSuffix(scope.selector("node_filesystem_files"), "}") + strings.TrimSuffix(dim, "}") + `,private="duplicate"}`, "values": "100 100 100"}
	duplicate = append(duplicate, row)
	tests = append(tests, map[string]any{"name": "duplicate inode source", "interval": "15s", "input_series": duplicate, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "30s", "exp_samples": expected(30, nil)}}})
	bind := inputs("100 100 100", "80 80 80", "0 0 0")
	exp := expected(30, []float64{100, 80, 20, 20})
	for _, raw := range inputs("100 100 100", "80 80 80", "0 0 0") {
		r := raw.(map[string]any)
		r["series"] = strings.Replace(r["series"].(string), `mountpoint="/"`, `mountpoint="/bind"`, 1)
		bind = append(bind, r)
	}
	for _, raw := range expected(30, []float64{100, 80, 20, 20}) {
		r := raw.(map[string]any)
		r["labels"] = strings.Replace(r["labels"].(string), `mountpoint="/"`, `mountpoint="/bind"`, 1)
		exp = append(exp, r)
	}
	tests = append(tests, map[string]any{"name": "bind mounts remain distinct", "interval": "15s", "input_series": bind, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "30s", "exp_samples": exp}}})
	exact, _ := NewPlan(keys, testTime, testTime, testTime, false, Dimensions{"device": "/dev/a", "mountpoint": "/", "fstype": "ext4"}, DefaultBudget())
	exactExpression, _ := exact.Expression(scope)
	tests = append(tests, map[string]any{"name": "exact inode mount selector", "interval": "15s", "input_series": bind, "promql_expr_test": []any{map[string]any{"expr": exactExpression, "eval_time": "30s", "exp_samples": expected(30, []float64{100, 80, 20, 20})}}})
	tests = append(tests, map[string]any{"name": "unmounted inode creates gaps", "interval": "15s", "input_series": inputs("100 stale _", "80 stale _", "0 stale _"), "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "30s", "exp_samples": []any{}}}})
	runPromtoolCases(t, "filesystem-inodes", tests)
	t.Logf("pinned promtool passed %d inode capacity/evidence scenarios", len(tests))
}

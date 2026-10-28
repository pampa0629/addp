package resourcequery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBlockCapacityInstantOnlyBudgetAndClosedDimensions(t *testing.T) {
	key := []string{"node.block.capacity_bytes"}
	p, e := NewPlan(key, testTime, testTime, testTime, false, nil, DefaultBudget())
	if e != nil || p.BlockGroups != 100 || p.SeriesUpperBound != 100 {
		t.Fatal(p, e)
	}
	if _, e = NewPlan(key, testTime.Add(-time.Minute), testTime, testTime, true, nil, DefaultBudget()); !errors.Is(e, ErrInvalid) {
		t.Fatal("capacity trend admitted", e)
	}
	for _, d := range []Dimensions{{"device": "vda"}, {"device": "vda", "kind": "physical"}, {"device": "vda", "fstype": "ext4"}} {
		if _, e = NewPlan(key, testTime, testTime, testTime, false, d, DefaultBudget()); !errors.Is(e, ErrInvalid) {
			t.Fatal(d, e)
		}
	}
	selected, e := NewPlan(key, testTime, testTime, testTime, false, Dimensions{"device": "vda", "kind": "device"}, DefaultBudget())
	if e != nil || selected.BlockGroups != 1 {
		t.Fatal(selected, e)
	}
	rows := []string{}
	for _, component := range []string{"value", "sampled_at", "observed_at"} {
		v := testTime.Unix()
		if component == "value" {
			v = 524288
		}
		rows = append(rows, fmt.Sprintf(`{"metric":{"addp_metric":%q,"addp_component":%q,"device":"vda","kind":"device"},"value":[%d,%q]}`, key[0], component, testTime.Unix(), fmt.Sprint(v)))
	}
	body := `{"status":"success","data":{"resultType":"vector","result":[` + strings.Join(rows, ",") + `]}}`
	normalized, e := normalize(wire(t, body), p, DefaultBudget())
	if e != nil || len(normalized) != 1 || normalized[0].Points[0].DataState != "valid" {
		t.Fatal(normalized, e)
	}
	if _, e = normalize(wire(t, strings.ReplaceAll(body, `"kind":"device"`, `"kind":"physical"`)), p, DefaultBudget()); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	b := DefaultBudget()
	b.MaxSeries = 1
	limited, _ := NewPlan(key, testTime, testTime, testTime, false, nil, b)
	duplicate := wire(t, body)
	extra := duplicate.Data.Result[0]
	extra.Metric = map[string]string{"addp_metric": key[0], "addp_component": "value", "device": "vdb", "kind": "device"}
	duplicate.Data.Result = append(duplicate.Data.Result, extra)
	if _, e = normalize(duplicate, limited, b); !errors.Is(e, ErrBudget) {
		t.Fatal("capacity truncated", e)
	}
}

func TestIntegrationMetricsBlockCapacity(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	scope := Scope{NodeID: testNode, Instance: "fixture:9100"}
	p, _ := NewPlan([]string{"node.block.capacity_bytes"}, testTime, testTime, testTime, false, nil, DefaultBudget())
	expression, e := p.Expression(scope)
	if e != nil {
		t.Fatal(e)
	}
	tests := []any{}
	for _, s := range []struct {
		name, sample, boot, success, count, capacity string
		valid                                        bool
		removed                                      bool
	}{
		{"fresh", "20", "1", "1", "1", "524288", true, false},
		{"stale-file", "1", "1", "1", "1", "524288", true, false}, // stamp survives; normalizer marks it stale
		{"failed", "20", "1", "0", "0", "524288", false, false},
		{"boot-mismatch", "20", "2", "1", "1", "524288", false, false},
		{"incomplete-frame", "20", "1", "1", "2", "524288", false, false},
		{"removed-device-lookback", "20", "1", "1", "0", "524288", false, true},
		{"invalid-capacity", "20", "1", "1", "1", "-1", false, false},
		{"future-time", "2000", "1", "1", "1", "524288", false, false},
		{"textfile-error", "20", "1", "1", "1", "524288", false, false},
	} {
		textfile := "0+0x2"
		if s.name == "textfile-error" {
			textfile = "1+0x2"
		}
		inputs := []any{map[string]any{"series": scope.selector("node_textfile_scrape_error"), "values": textfile}}
		for name, value := range map[string]string{"supported": "1", "success": s.success, "sample_time_seconds": s.sample, "boot_time_seconds": s.boot, "device_count": s.count} {
			inputs = append(inputs, map[string]any{"series": scope.selector("addp_block_capacity_" + name), "values": value + "+0x2"})
		}
		inputs = append(inputs, map[string]any{"series": scope.selector("node_boot_time_seconds"), "values": "1+0x2"})
		raw := strings.TrimSuffix(scope.selector("addp_block_capacity_bytes"), "}") + `,device="vda",kind="device"}`
		values := s.capacity + "+0x2"
		if s.removed {
			values = s.capacity + " _ _"
		}
		inputs = append(inputs, map[string]any{"series": raw, "values": values})
		expected := []any{}
		if s.valid {
			for _, component := range []string{"value", "sampled_at", "observed_at"} {
				value := 524288.
				if component != "value" {
					if s.name == "stale-file" {
						value = 1
					} else {
						value = 20
					}
				}
				expected = append(expected, map[string]any{"labels": fmt.Sprintf(`{addp_metric="node.block.capacity_bytes",addp_component=%q,device="vda",kind="device"}`, component), "value": value})
			}
		}
		eval := "30s"
		if s.name == "stale-file" {
			eval = "90s"
		}
		tests = append(tests, map[string]any{"name": s.name, "interval": "15s", "input_series": inputs, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": eval, "exp_samples": expected}}})
	}
	runPromtoolCases(t, "block-capacity", tests, false)
}

func TestIntegrationMetricsBlockCapacitySource(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	read := func(name string) []byte {
		b, e := os.ReadFile(os.Getenv(name))
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	certificate, e := tls.X509KeyPair(read("MONITOR_PROMETHEUS_CLIENT_CERT_FILE"), read("MONITOR_PROMETHEUS_CLIENT_KEY_FILE"))
	if e != nil {
		t.Fatal(e)
	}
	client, e := NewClient(os.Getenv("MONITOR_PROMETHEUS_URL"), read("MONITOR_PROMETHEUS_CA_FILE"), certificate)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	var expected map[string]float64
	if e = json.Unmarshal([]byte(os.Getenv("ADDP_BLOCK_CAPACITY_EXPECTED")), &expected); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Second)
	b := DefaultBudget()
	scope := Scope{NodeID: os.Getenv("ADDP_METRICS_QUERY_NODE_ID"), Instance: os.Getenv("ADDP_METRICS_QUERY_INSTANCE")}
	p, e := NewPlan([]string{"node.block.capacity_bytes"}, now, now, now, false, nil, b)
	if e != nil {
		t.Fatal(e)
	}
	collection, e := client.Collection(context.Background(), scope, now, b, true)
	if e != nil || collection.State != "collecting" || (collection.BlockCapacity != "available" && !(len(expected) == 0 && collection.BlockCapacity == "empty")) {
		t.Fatal(collection, e)
	}
	rows, e := client.Query(context.Background(), p, scope, b)
	if e != nil {
		t.Fatal(e)
	}
	if len(expected) == 0 {
		if len(rows) != 1 || len(rows[0].Dimensions) != 0 || rows[0].Points[0].DataState != "no_data" {
			t.Fatal(rows)
		}
		return
	}
	if len(rows) != len(expected) {
		t.Fatal("partial device list", rows, expected)
	}
	for _, r := range rows {
		point := r.Points[0]
		wanted, ok := expected[r.Dimensions["device"]+":"+r.Dimensions["kind"]]
		if !ok || point.DataState != "valid" || point.Value == nil || *point.Value != wanted {
			t.Fatal(r, expected)
		}
	}
}

func TestBlockCapacityCollectionIsOptionalAndRequiresFreshCompleteFrame(t *testing.T) {
	scope := Scope{NodeID: testNode, Instance: "fixture:9100"}
	if strings.Contains(collectionExpression(scope, false), "addp_block_capacity_") {
		t.Fatal("optional block query leaked into core collection")
	}
	if !strings.Contains(collectionExpression(scope, true), "addp_block_capacity_sample_time_seconds") {
		t.Fatal("source time omitted")
	}
	for _, s := range []struct {
		name   string
		fields map[string]string
		state  string
	}{
		{"disabled", map[string]string{"block_count": "0", "block_collector_count": "0"}, "not_collected"},
		{"missing-file", map[string]string{"block_count": "0", "block_collector_count": "1"}, "failed"},
		{"valid", map[string]string{"block_count": "1", "block_time": fmt.Sprint(testTime.Unix()), "block_ready": fmt.Sprint(testTime.Unix()), "block_supported": "1", "block_devices": "2"}, "available"},
		{"empty", map[string]string{"block_count": "1", "block_time": fmt.Sprint(testTime.Unix()), "block_ready": fmt.Sprint(testTime.Unix()), "block_supported": "1", "block_devices": "0"}, "empty"},
		{"incomplete", map[string]string{"block_count": "1", "block_time": fmt.Sprint(testTime.Unix()), "block_ready": fmt.Sprint(testTime.Unix()), "block_supported": "1"}, "failed"},
		{"unprovable", map[string]string{"block_count": "1", "block_time": fmt.Sprint(testTime.Unix()), "block_supported": "0"}, "unsupported"},
		{"old unsupported", map[string]string{"block_count": "1", "block_time": fmt.Sprint(testTime.Unix() - 61), "block_supported": "0"}, "stale"},
		{"future", map[string]string{"block_count": "1", "block_time": fmt.Sprint(testTime.Unix() + 1), "block_supported": "0"}, "failed"},
	} {
		fields := map[string]string{"up_count": "1", "up": "1", "sampled_at": fmt.Sprint(testTime.Unix()), "filesystem_count": "0", "network_count": "0"}
		for k, v := range s.fields {
			fields[k] = v
		}
		got, e := normalizeCollection(collectionWire(t, fields), testTime)
		if e != nil || got.BlockCapacity != s.state || got.State != "collecting" {
			t.Fatal(s.name, got, e)
		}
	}
}

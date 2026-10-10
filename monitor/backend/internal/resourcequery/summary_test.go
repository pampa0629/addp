package resourcequery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSummaryBatchQueriesFixedFormulasAndStrictlySeparatesNodes(t *testing.T) {
	second := "22222222-2222-4222-8222-222222222222"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/v1/query" || r.URL.RawQuery != "" || r.ParseForm() != nil {
			t.Error("batch did not use bounded form query")
		}
		expression := r.PostForm.Get("query")
		if !strings.Contains(expression, `addp_node_id="`+testNode+`"`) || !strings.Contains(expression, `addp_node_id="`+second+`"`) || strings.Contains(expression, "node_load") {
			t.Error("fixed per-node scope lost")
		}
		data := envelope{}
		data.Status, data.Data.ResultType, data.Data.Result = "success", "vector", []wireSeries{}
		if strings.Contains(expression, "up_count") {
			for _, id := range []string{testNode, second} {
				fields := map[string]string{"up_count": "0", "filesystem_count": "0", "network_count": "0"}
				if id == testNode {
					fields["up_count"], fields["up"], fields["sampled_at"] = "1", "1", fmt.Sprint(testTime.Unix())
				}
				for _, row := range collectionWire(t, fields).Data.Result {
					row.Metric["addp_summary_node"] = id
					data.Data.Result = append(data.Data.Result, row)
				}
			}
		} else {
			for _, key := range SummaryMetrics() {
				for _, component := range []string{"value", "sampled_at"} {
					raw := "0"
					if component == "sampled_at" {
						raw = fmt.Sprint(testTime.Unix())
					}
					var pair sample
					_ = json.Unmarshal([]byte(fmt.Sprintf(`[%d,%q]`, testTime.Unix(), raw)), &pair)
					data.Data.Result = append(data.Data.Result, wireSeries{Metric: map[string]string{"addp_metric": key, "addp_component": component, "addp_summary_node": testNode}, Value: &pair})
				}
			}
		}
		rows := []map[string]any{}
		for _, row := range data.Data.Result {
			rows = append(rows, map[string]any{"metric": row.Metric, "value": row.Value})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": rows}})
	}))
	defer server.Close()
	c := &Client{origin: server.URL, http: server.Client()}
	rows, err := c.Summaries(context.Background(), []Scope{{testNode, "host-a:9100"}, {second, "host-b:9100"}}, testTime, DefaultBudget())
	if err != nil || calls != 2 || len(rows) != 2 || rows[testNode].Collection.State != "collecting" || rows[second].Collection.State != "no_sample" {
		t.Fatal(rows, err, calls)
	}
	for _, row := range rows[testNode].Series {
		if row.Points[0].DataState != "valid" || *row.Points[0].Value != 0 {
			t.Fatal("valid zero lost", row)
		}
	}
	for _, row := range rows[second].Series {
		if row.Points[0].DataState != "no_data" || row.Points[0].Value != nil {
			t.Fatal("another host's data reused", row)
		}
	}
}

func TestSummaryBudgetsAndWireLabelsAreClosed(t *testing.T) {
	b := DefaultBudget()
	if _, err := NewSummaryPlan(50, testTime, b); err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{0, 51, 101} {
		if _, err := NewSummaryPlan(count, testTime, b); err == nil {
			t.Fatal("unbounded count accepted", count)
		}
	}
	b.MaxTotalPoints = 3
	if _, err := NewSummaryPlan(2, testTime, b); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	data := collectionWire(t, map[string]string{"up_count": "0"})
	if _, err := splitSummaryEvidence(data, map[string]bool{testNode: true}, 9, "addp_summary_node"); err == nil {
		t.Fatal("missing scope label accepted")
	}
	data.Data.Result[0].Metric["addp_summary_node"] = "foreign"
	if _, err := splitSummaryEvidence(data, map[string]bool{testNode: true}, 9, "addp_summary_node"); err == nil {
		t.Fatal("foreign node accepted")
	}
	data.Data.Result[0].Metric["addp_summary_node"] = testNode
	data.Data.Result[0].Metric["private_address"] = "unexpected"
	groups, err := splitSummaryEvidence(data, map[string]bool{testNode: true}, 9, "addp_summary_node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeCollection(groups[testNode], testTime); err == nil {
		t.Fatal("extra labels accepted")
	}
	data.Data.Result = append(data.Data.Result, data.Data.Result[0])
	if _, err := splitSummaryEvidence(data, map[string]bool{testNode: true}, 1, "addp_summary_node"); !errors.Is(err, ErrBudget) {
		t.Fatal("per-node wire bound bypass", err)
	}
}

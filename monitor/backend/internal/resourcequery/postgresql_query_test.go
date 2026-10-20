package resourcequery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func queryPostgreSQLScope() PostgreSQLScope {
	started := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	return PostgreSQLScope{DeploymentID: "10000000-0000-4000-8000-000000000001", Instance: "127.0.0.1:9443", Source: models.PostgreSQLMetricsSource{State: "available", Endpoint: "https://127.0.0.1:9443/metrics", ContainerID: strings.Repeat("a", 64), StartedAt: &started}}
}

func TestPostgreSQLQueriesRequireSnapshotAndCompleteCounterGeneration(t *testing.T) {
	now := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	keys := []string{}
	for _, d := range PostgreSQLCatalog() {
		keys = append(keys, d.Key)
	}
	p, err := NewPostgreSQLPlan(keys, now, now, now, false, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	expression, err := queryPostgreSQLScope().expression(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`addp_monitor_kind="engine_resources"`, `addp_database_container_id="` + strings.Repeat("a", 64) + `"`, `addp_database_container_started_at=`, "sample_time_seconds", "server_start_time_seconds", "statistics_info", `count_over_time(`, `>=4`, `resets(`, `offset 1m`, `track_counts_enabled`, `timestamp(`} {
		if !strings.Contains(expression, part) {
			t.Fatalf("missing snapshot/window guard %s", part)
		}
	}
	calls := 0
	validSnapshot := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.RawQuery != "" || r.ParseForm() != nil {
			t.Error("read expression requires bounded form transport")
		}
		rows := []map[string]interface{}{}
		add := func(labels map[string]string, value string) {
			rows = append(rows, map[string]interface{}{"metric": labels, "value": []interface{}{now.Unix(), value}})
		}
		if strings.Contains(r.PostForm.Get("query"), `"signal"`) {
			for key, value := range map[string]string{"up_count": "1", "up": "1", "sampled_at": fmt.Sprint(now.Unix()), "snapshot_valid": map[bool]string{true: "1", false: "0"}[validSnapshot]} {
				add(map[string]string{"signal": key}, value)
			}
		} else {
			for _, d := range p.Metrics {
				add(map[string]string{"addp_metric": d.Key, "addp_component": "value"}, "0")
				add(map[string]string{"addp_metric": d.Key, "addp_component": "sampled_at"}, fmt.Sprint(now.Unix()))
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "vector", "result": rows}})
	}))
	defer server.Close()
	c := &Client{origin: server.URL, http: server.Client()}
	result, err := c.PostgreSQLQuery(context.Background(), p, queryPostgreSQLScope(), DefaultBudget())
	if err != nil || result.Collection.State != "snapshot_unavailable" || calls != 2 {
		t.Fatalf("SQL failure evidence %+v %v", result.Collection, err)
	}
	for _, series := range result.Series {
		if series.Points[0].Value != nil {
			t.Fatal("HTTP up=1 exposed invalid SQL snapshot")
		}
	}
	validSnapshot = true
	result, err = c.PostgreSQLQuery(context.Background(), p, queryPostgreSQLScope(), DefaultBudget())
	if err != nil || result.Collection.State != "collecting" || len(result.Series) != 12 {
		t.Fatal("valid snapshot lost", err)
	}
	for _, series := range result.Series {
		if series.Points[0].DataState != "valid" || series.Points[0].Value == nil || *series.Points[0].Value != 0 {
			t.Fatal("valid zero missing")
		}
	}
}

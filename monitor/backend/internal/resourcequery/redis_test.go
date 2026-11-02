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
	"time"
)

func TestRedisFixedCatalogPreservesQueryBudgetAndTypeIsolation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	keys := []string{}
	for _, d := range RedisCatalog() {
		keys = append(keys, d.Key)
	}
	if len(keys) != 22 {
		t.Fatal("Redis catalog size", len(keys))
	}
	if _, err := NewEnginePlan("redis", keys, now, now, now, false, DefaultBudget()); !errors.Is(err, ErrBudget) {
		t.Fatal("all-metric query bypassed budget", err)
	}
	for _, bounds := range [][2]int{{0, 10}, {10, 17}, {17, 22}} {
		if _, err := NewEnginePlan("redis", keys[bounds[0]:bounds[1]], now, now, now, false, DefaultBudget()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewEnginePlan("redis", []string{"postgresql.connections.total"}, now, now, now, false, DefaultBudget()); !errors.Is(err, ErrInvalid) {
		t.Fatal("cross-type metric accepted", err)
	}
	if _, err := NewEngineSummaryPlan("redis", 26, now, DefaultBudget()); !errors.Is(err, ErrBudget) {
		t.Fatal("summary bypassed total budget", err)
	}
	scope := queryEngineScope()
	scope.EngineType = "redis"
	p, err := NewEnginePlan("redis", []string{"redis.commands.per_second", "redis.memory.used_percent"}, now, now, now, false, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	expression, err := scope.expression(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`addp_source="redis"`, `counter_epoch=~"[0-9a-f]{64}"`, `count_over_time(`, `>=4`, `resets(`, `offset 1m`, `maxmemory_bytes`, `>0`, `memory_excluded_from_limit_bytes`} {
		if !strings.Contains(expression, part) {
			t.Fatal("guard missing", part)
		}
	}
}

func TestMinIOCatalogPreservesGroupsBudgetAndTypeIsolation(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	keys := []string{}
	for _, metric := range MinIOCatalog() {
		keys = append(keys, metric.Key)
	}
	if len(keys) != 17 {
		t.Fatal("MinIO catalog size", len(keys))
	}
	for _, group := range [][]string{keys[:10], keys[10:]} {
		if _, err := NewEnginePlan("minio", group, at, at, at, false, DefaultBudget()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewEnginePlan("minio", keys, at, at, at, false, DefaultBudget()); !errors.Is(err, ErrBudget) {
		t.Fatal("whole catalog bypassed budget", err)
	}
	if _, err := NewEngineSummaryPlan("minio", 26, at, DefaultBudget()); !errors.Is(err, ErrBudget) {
		t.Fatal("summary bypassed budget", err)
	}
	for _, key := range []string{"postgresql.connections.total", "redis.commands.per_second", "minio.scan.bucket_name"} {
		if _, err := NewEnginePlan("minio", []string{key}, at, at, at, false, DefaultBudget()); !errors.Is(err, ErrInvalid) {
			t.Fatal("non-catalog metric", err)
		}
	}
}

func TestEngineValuesKeepCapacityCountsAndStatesInTheirDomains(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		key, value string
		valid      bool
	}{{"redis.memory.used_percent", "180", true}, {"redis.keyspace.hit_percent", "180", false}, {"redis.persistence.aof_enabled", "0.5", false}, {"redis.persistence.aof_enabled", "0", true}, {"minio.capacity.used_percent", "101", false}, {"minio.drives.online", "0.5", false}, {"minio.scan.objects", "9007199254740992", false}, {"minio.drives.offline", "0", true}} {
		p, err := NewEnginePlan(strings.Split(tc.key, ".")[0], []string{tc.key}, at, at, at, false, DefaultBudget())
		if err != nil {
			t.Fatal(err)
		}
		raw := fmt.Sprintf(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"addp_metric":%q,"addp_component":"value"},"value":[%d,%q]},{"metric":{"addp_metric":%q,"addp_component":"sampled_at"},"value":[%d,%q]}]}}`, tc.key, at.Unix(), tc.value, tc.key, at.Unix(), fmt.Sprint(at.Unix()))
		var data envelope
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			t.Fatal(err)
		}
		rows, err := normalize(data, p, DefaultBudget())
		if err != nil {
			t.Fatal(err)
		}
		if (rows[0].Points[0].Value != nil) != tc.valid {
			t.Fatal("value domain changed", tc.key, rows)
		}
	}
}

func TestEngineSummaryTransportSeparatesTypesAndRejectsForeignEvidence(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	postgres := queryEngineScope()
	redis := postgres
	redis.EngineType = "redis"
	redis.DeploymentID = "10000000-0000-4000-8000-000000000002"
	redis.Instance = "127.0.0.2:9443"
	minio := redis
	minio.EngineType = "minio"
	minio.DeploymentID = "10000000-0000-4000-8000-000000000003"
	minio.Instance = "127.0.0.3:9443"
	for _, mode := range []string{"valid", "redis-snapshot-failed", "minio-snapshot-failed", "foreign-id", "cross-type-metric"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/query" || r.URL.RawQuery != "" || r.ParseForm() != nil {
					t.Error("batch query left the shared form transport")
				}
				expression := r.PostForm.Get("query")
				if !strings.Contains(expression, `addp_source="postgresql"`) || !strings.Contains(expression, `addp_source="redis"`) || !strings.Contains(expression, `addp_source="minio"`) {
					t.Error("batch lost an authoritative engine type")
				}
				rows := []map[string]any{}
				add := func(id string, labels map[string]string, value string) {
					labels["addp_summary_engine"] = id
					rows = append(rows, map[string]any{"metric": labels, "value": []any{at.Unix(), value}})
				}
				for _, scope := range []EngineScope{postgres, redis, minio} {
					id := scope.DeploymentID
					if mode == "foreign-id" && scope.EngineType == "redis" {
						id = "10000000-0000-4000-8000-000000000099"
					}
					if strings.Contains(expression, `"signal"`) {
						valid := "1"
						if (mode == "redis-snapshot-failed" && scope.EngineType == "redis") || (mode == "minio-snapshot-failed" && scope.EngineType == "minio") {
							valid = "0"
						}
						for key, value := range map[string]string{"up_count": "1", "up": "1", "sampled_at": fmt.Sprint(at.Unix()), "snapshot_valid": valid} {
							add(id, map[string]string{"signal": key}, value)
						}
						continue
					}
					plan, err := NewEngineSummaryPlan(scope.EngineType, 1, at, DefaultBudget())
					if err != nil {
						t.Error(err)
						return
					}
					for _, metric := range plan.Metrics {
						key, value := metric.Key, "0"
						if key == "redis.memory.used_percent" {
							value = "180"
						}
						if mode == "cross-type-metric" && key == "redis.connections.total" {
							key = "postgresql.connections.total"
						}
						add(id, map[string]string{"addp_metric": key, "addp_component": "value"}, value)
						add(id, map[string]string{"addp_metric": key, "addp_component": "sampled_at"}, fmt.Sprint(at.Unix()))
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": rows}})
			}))
			defer server.Close()
			client := &Client{origin: server.URL, http: server.Client()}
			result, err := client.EngineSummaries(context.Background(), []EngineScope{postgres, redis, minio}, at, DefaultBudget())
			if mode == "foreign-id" || mode == "cross-type-metric" {
				if err == nil || result != nil {
					t.Fatal("foreign batch evidence returned values", err)
				}
				return
			}
			if err != nil || calls != 2 || len(result) != 3 {
				t.Fatal("batch did not use two shared reads", calls, err)
			}
			for _, scope := range []EngineScope{postgres, redis, minio} {
				row := result[scope.DeploymentID]
				failed := (mode == "redis-snapshot-failed" && scope.EngineType == "redis") || (mode == "minio-snapshot-failed" && scope.EngineType == "minio")
				expectedState := "collecting"
				if failed {
					expectedState = "snapshot_unavailable"
				}
				if row.Collection.State != expectedState || len(row.Series) != 4 {
					t.Fatal("collection failure crossed deployment boundary", row.Collection)
				}
				for _, series := range row.Series {
					if !strings.HasPrefix(series.MetricKey, scope.EngineType+".") {
						t.Fatal("cross-type series returned", series.MetricKey)
					}
					point := series.Points[0]
					if failed {
						if point.Value != nil || point.SampledAt != nil || point.DataState != "no_data" {
							t.Fatal("invalid snapshot retained a value", point)
						}
						continue
					}
					expected := float64(0)
					if series.MetricKey == "redis.memory.used_percent" {
						expected = 180
					}
					if point.Value == nil || *point.Value != expected || point.DataState != "valid" {
						t.Fatal("valid zero or over-limit percentage lost", point)
					}
				}
			}
		})
	}
}

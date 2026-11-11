package resourcequery

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func cdcQueryScope(kind string) CDCScope {
	started := time.Unix(0, 0).UTC()
	return CDCScope{Instance: "127.0.0.1:9443", Component: models.ObservabilityComponentIdentity{DockerEngineID: "fixture", Project: "addp-infra", Kind: kind, ContainerID: strings.Repeat("a", 64), StartedAt: started, Metrics: &models.EngineMetricsSource{State: "available", Endpoint: "https://127.0.0.1:9443/metrics", ContainerID: strings.Repeat("a", 64), StartedAt: &started}}}
}
func TestCDCPlansAndClosedCatalogs(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, kind := range []string{"redpanda", "kafka-connect"} {
		for _, def := range CDCCatalog(kind) {
			p, err := NewCDCPlan(kind, []string{def.Key}, now, now, now, false, DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			expression, err := cdcQueryScope(kind).expression(p)
			if err != nil || !strings.Contains(expression, `addp_component_id=`) || !strings.Contains(expression, `addp_component_container_started_at=`) || strings.Contains(expression, "addp_deployment_id") {
				t.Fatal(expression, err)
			}
			p.Metrics[0].Unit = "forged"
			if _, err = cdcQueryScope(kind).expression(p); err != ErrInvalid {
				t.Fatal("forged definition accepted", err)
			}
		}
		for _, keys := range [][]string{{"up"}, {"cdc.connect.tasks", "cdc.connect.tasks"}, {"cdc.redpanda.disk_total_bytes", "cdc.connect.tasks"}} {
			if _, err := NewCDCPlan(kind, keys, now, now, now, false, DefaultBudget()); err == nil {
				t.Fatal(keys)
			}
		}
	}
}

// Runs inside the existing disposable CDC gate. Real Prometheus evaluates fixed
// samples; native source transport and identity admission have separate proofs.
func TestIntegrationCDCMetricQueries(t *testing.T) {
	if os.Getenv("ADDP_CDC_ADMISSION_FILE") == "" {
		t.Skip("requires owned CDC gate")
	}
	project, work, compose := os.Getenv("METRICS_T2_PROJECT"), os.Getenv("METRICS_T2_WORK"), os.Getenv("METRICS_T2_COMPOSE")
	if !regexp.MustCompile(`^addp-metrics-t2-[0-9a-f]{32}$`).MatchString(project) || work == "" || compose == "" {
		t.Fatal("missing owned fixture")
	}
	type input struct {
		Series string `json:"series"`
		Values string `json:"values"`
	}
	tests := []any{}
	for _, kind := range []string{"redpanda", "kafka-connect"} {
		scope := cdcQueryScope(kind)
		baseline := func() []input {
			rows := []input{{scope.selector("up"), "1x8"}, {scope.selector("observed_at_seconds"), "0+15x8"}}
			fields := map[string]string{"disk_total_bytes": "100x8", "disk_free_bytes": "25x8", "unavailable_partitions": "0x8", "consumer_group_partitions": "1x8", "consumer_group_pending_records": "4x8"}
			if kind == "kafka-connect" {
				fields = map[string]string{"connectors": "2x8", "tasks": "2x8", "offset_commit_tasks": "1x8", "offset_commit_failure_ratio_max": "0.25x8", "streaming_connectors": "1x8", "streaming_connected": "1x8", "source_delay_connectors": "1x8", "source_delay_max_seconds": "-0.002x8"}
				for field, states := range map[string][]string{"connector_states": {"FAILED"}, "task_states": {"FAILED", "RUNNING", "PAUSED"}} {
					for _, state := range states {
						v := "0x8"
						if state != "PAUSED" {
							v = "1x8"
						}
						rows = append(rows, input{strings.TrimSuffix(scope.selector(field), "}") + `,state=` + strconv.Quote(state) + `}`, v})
					}
				}
			}
			for field, value := range fields {
				rows = append(rows, input{scope.selector(field), value})
			}
			return rows
		}
		for _, def := range CDCCatalog(kind) {
			for _, change := range []string{"valid", "missing", "duplicate", "down", "old_observation", "wrong_runtime", "wrong_stamp"} {
				rows := baseline()
				current := scope
				switch change {
				case "missing":
					rows = []input{{scope.selector("up"), "1x8"}, {scope.selector("observed_at_seconds"), "0+15x8"}}
				case "duplicate":
					rows = append(rows, input{strings.TrimSuffix(scope.selector("up"), "}") + `,forged="duplicate"}`, "1x8"})
				case "down":
					rows[0].Values = "1x7 0"
				case "old_observation":
					rows[1].Values = "0x8"
				case "wrong_runtime":
					current.Component.ContainerID = strings.Repeat("b", 64)
					current.Component.Metrics = &models.EngineMetricsSource{State: "available", Endpoint: scope.Component.Metrics.Endpoint, ContainerID: current.Component.ContainerID, StartedAt: &current.Component.StartedAt}
				case "wrong_stamp":
					rows[1].Values = "0+15x7 _"
				}
				at := time.Unix(120, 0).UTC()
				p, err := NewCDCPlan(kind, []string{def.Key}, at, at, at, false, DefaultBudget())
				if err != nil {
					t.Fatal(err)
				}
				expr, err := current.expression(p)
				if err != nil {
					t.Fatal(err)
				}
				samples := []any{}
				if change == "valid" {
					value := map[string]float64{"cdc.redpanda.disk_total_bytes": 100, "cdc.redpanda.disk_free_bytes": 25, "cdc.redpanda.disk_used_percent": 75, "cdc.redpanda.unavailable_partitions": 0, "cdc.redpanda.consumer_group_partitions": 1, "cdc.redpanda.consumer_group_pending_records": 4, "cdc.connect.connectors": 2, "cdc.connect.tasks": 2, "cdc.connect.failed_connectors": 1, "cdc.connect.failed_tasks": 1, "cdc.connect.running_tasks": 1, "cdc.connect.paused_tasks": 0, "cdc.connect.offset_commit_tasks": 1, "cdc.connect.offset_commit_failure_percent": 25, "cdc.connect.streaming_connectors": 1, "cdc.connect.streaming_connected": 1, "cdc.connect.source_delay_connectors": 1, "cdc.connect.source_delay_max_seconds": -0.002}[def.Key]
					for _, component := range []string{"value", "sampled_at"} {
						v := value
						if component == "sampled_at" {
							v = 120
						}
						samples = append(samples, map[string]any{"labels": `{addp_metric=` + strconv.Quote(def.Key) + `,addp_component=` + strconv.Quote(component) + `}`, "value": v})
					}
				}
				tests = append(tests, map[string]any{"name": def.Key + "/" + change, "interval": "15s", "input_series": rows, "promql_expr_test": []any{map[string]any{"expr": expr, "eval_time": "2m", "exp_samples": samples}}})
			}
		}
	}
	document, err := json.Marshal(map[string]any{"evaluation_interval": "15s", "tests": tests})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(work, "query-tests", "cdc-rules.json")
	if err = os.WriteFile(path, document, 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "--env-file", "/dev/null", "-p", project, "-f", compose, "run", "--rm", "--no-deps", "cdc-query-tool")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 8000 {
			out = out[:8000]
		}
		t.Fatalf("CDC PromQL proof: %v\n%s", err, out)
	}
	t.Logf("real Prometheus passed %d CDC query scenarios", len(tests))
}

func TestCDCScalarNormalizerKeepsNegativeNativeDelayOnly(t *testing.T) {
	at := time.Unix(120, 0).UTC()
	for _, tc := range []struct {
		key, raw string
		valid    bool
	}{{"cdc.connect.source_delay_max_seconds", "-0.002", true}, {"cdc.connect.tasks", "0", true}, {"cdc.connect.tasks", "-1", false}, {"cdc.connect.tasks", "0.5", false}, {"cdc.connect.offset_commit_failure_percent", "101", false}, {"cdc.connect.source_delay_max_seconds", "NaN", false}} {
		p, err := NewCDCPlan("kafka-connect", []string{tc.key}, at, at, at, false, DefaultBudget())
		if err != nil {
			t.Fatal(err)
		}
		raw := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"addp_metric":` + strconv.Quote(tc.key) + `,"addp_component":"value"},"value":[120,` + strconv.Quote(tc.raw) + `]},{"metric":{"addp_metric":` + strconv.Quote(tc.key) + `,"addp_component":"sampled_at"},"value":[120,"120"]}]}}`
		var data envelope
		if err = json.Unmarshal([]byte(raw), &data); err != nil {
			t.Fatal(err)
		}
		result, err := normalize(data, p, DefaultBudget())
		if tc.valid {
			if err != nil || result[0].Points[0].DataState != "valid" {
				t.Fatalf("valid CDC scalar lost: %s %+v %v", tc.key, result, err)
			}
		} else if err == nil && (result[0].Points[0].DataState != "no_data" || result[0].Points[0].Value != nil || result[0].Points[0].SampledAt != nil) {
			t.Fatalf("invalid CDC scalar admitted: %s=%s", tc.key, tc.raw)
		}
	}
}

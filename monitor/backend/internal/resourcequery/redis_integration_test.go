package resourcequery

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
)

// The existing owned Redis gate also owns the offline Prometheus query engine.
// Fixed samples prove expression semantics; this does not claim a live HTTP SD chain.
func TestIntegrationRedisMetricQueries(t *testing.T) {
	project := os.Getenv("ADDP_REDIS_T2_PROJECT")
	if os.Getenv("ADDP_REDIS_OBSERVATION_T2") != "1" {
		t.Skip("requires owned Redis source gate")
	}
	if !strings.HasPrefix(project, "addp-redis-t2-") {
		t.Fatal("invalid disposable project")
	}
	started := time.Unix(0, 0).UTC()
	scope := EngineScope{EngineType: "redis", DeploymentID: "10000000-0000-4000-8000-000000000001", Instance: "127.0.0.1:9443", Source: models.EngineMetricsSource{State: "available", Endpoint: "https://127.0.0.1:9443/metrics", ContainerID: strings.Repeat("a", 64), StartedAt: &started}}
	type input struct {
		Series string `json:"series"`
		Values string `json:"values"`
	}
	raw := func(field, epoch string) string {
		s := scope.selector(field)
		if epoch != "" {
			s = strings.TrimSuffix(s, "}") + `,counter_epoch=` + strconv.Quote(epoch) + `}`
		}
		return s
	}
	baseline := func(aofEnabled bool) []input {
		rows := []input{{scope.selector("up"), "1x8"}}
		for _, field := range redisFields {
			value := "0x8"
			epoch := ""
			switch field {
			case "observed_at_seconds", "uptime_seconds":
				value = "0+15x8"
			case "memory_allocated_bytes":
				value = "100x8"
			case "memory_excluded_from_limit_bytes":
				value = "20x8"
			case "memory_counted_for_limit_bytes":
				value = "80x8"
			case "memory_rss_bytes":
				value = "120x8"
			case "maxmemory_bytes", "max_connections":
				value = "100x8"
			case "aof_enabled":
				if aofEnabled {
					value = "1x8"
				}
			case "rdb_last_bgsave_ok":
				value = "1x8"
			}
			for _, counter := range redisCounters {
				if field == counter {
					epoch = strings.Repeat("a", 64)
					value = "0+15x8"
					if field == "commands_total" {
						value = "0+30x8"
					}
				}
			}
			rows = append(rows, input{raw(field, epoch), value})
		}
		if aofEnabled {
			rows = append(rows, input{raw("aof_last_write_ok", ""), "1x8"}, input{raw("aof_last_rewrite_ok", ""), "1x8"})
		}
		return rows
	}
	type queryCase struct {
		name, key, change string
		value             *float64
	}
	cases := []queryCase{}
	value := func(v float64) *float64 { return &v }
	// Prometheus extrapolates a 45-second observed interval to the minute,
	// then divides by 60. Keep native float rounding in this exact assertion.
	factor := 60.0 / 45.0
	factor /= 60.0
	commandRate := 90.0 * factor
	for _, row := range []struct {
		key   string
		value float64
	}{{"redis.memory.used_percent", 80}, {"redis.commands.per_second", commandRate}, {"redis.keyspace.hit_percent", 50}, {"redis.persistence.aof_write_ok", 1}, {"redis.uptime_seconds", 120}} {
		cases = append(cases, queryCase{row.key, row.key, "", value(row.value)})
	}
	cases = append(cases,
		queryCase{"aof_off_memory", "redis.memory.allocated_bytes", "aof_off", value(100)},
		queryCase{"aof_off_commands", "redis.commands.per_second", "aof_off", value(commandRate)},
		queryCase{"aof_off_switch", "redis.persistence.aof_enabled", "aof_off", value(0)},
		queryCase{"aof_off_status_absent", "redis.persistence.aof_write_ok", "aof_off", nil},
		queryCase{"aof_on_missing_status", "redis.memory.allocated_bytes", "aof_missing", nil},
		queryCase{"aof_off_unexpected_status", "redis.memory.allocated_bytes", "aof_unexpected", nil},
	)
	for _, row := range []struct{ name, key, change string }{{"no_memory_limit", "redis.memory.used_percent", "limit"}, {"no_lookups", "redis.keyspace.hit_percent", "lookups"}, {"counter_reset", "redis.commands.per_second", "reset"}, {"new_epoch", "redis.commands.per_second", "epoch"}, {"missing_boundary", "redis.commands.per_second", "boundary"}, {"duplicate_epoch", "redis.memory.allocated_bytes", "duplicate"}, {"wrong_identity", "redis.memory.allocated_bytes", "identity"}, {"invalid_state", "redis.memory.allocated_bytes", "state"}, {"missing_field", "redis.memory.allocated_bytes", "missing"}, {"missing_epoch", "redis.memory.allocated_bytes", "noepoch"}} {
		cases = append(cases, queryCase{row.name, row.key, row.change, nil})
	}
	tests := []any{}
	for _, tc := range cases {
		rows := baseline(tc.change != "aof_off" && tc.change != "aof_unexpected")
		if tc.change == "aof_unexpected" {
			rows = append(rows, input{raw("aof_last_write_ok", ""), "1x8"})
		}
		for i := range rows {
			r := &rows[i]
			if tc.change == "state" && r.Series == scope.selector("loading") {
				r.Values = "0.5x8"
			}
			if tc.change == "missing" && r.Series == scope.selector("loading") {
				r.Values = "0x7 stale"
			}
			if tc.change == "aof_missing" && r.Series == scope.selector("aof_last_write_ok") {
				r.Values = "1x7 stale"
			}
			if tc.change == "noepoch" && strings.Contains(r.Series, "addp_redis_commands_total{") {
				r.Series = scope.selector("commands_total")
			}
			if tc.change == "limit" && r.Series == scope.selector("maxmemory_bytes") {
				r.Values = "0x8"
			}
			if tc.change == "lookups" && (strings.Contains(r.Series, "addp_redis_keyspace_hits_total{") || strings.Contains(r.Series, "addp_redis_keyspace_misses_total{")) {
				r.Values = "0x8"
			}
			if tc.change == "reset" && strings.Contains(r.Series, "addp_redis_commands_total{") {
				r.Values = "0+30x4 0+30x3"
			}
			if tc.change == "boundary" && r.Series == scope.selector("up") {
				r.Values = "1x3 stale 1x3"
			}
		}
		if tc.change == "epoch" {
			for _, field := range redisCounters {
				for i := range rows {
					if rows[i].Series == raw(field, strings.Repeat("a", 64)) {
						rows[i].Values = "0+30x4 stale _x2"
					}
				}
				rows = append(rows, input{raw(field, strings.Repeat("b", 64)), "_x4 0+30x3"})
			}
		}
		if tc.change == "duplicate" {
			rows = append(rows, input{raw("commands_total", strings.Repeat("b", 64)), "0+30x8"})
		}
		current := scope
		if tc.change == "identity" {
			current.Instance = "127.0.0.2:9443"
		}
		plan, err := NewEnginePlan("redis", []string{tc.key}, time.Unix(120, 0), time.Unix(120, 0), time.Unix(120, 0), false, DefaultBudget())
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		expression, err := current.expression(plan)
		if err != nil {
			t.Fatal(err)
		}
		expected := []any{}
		if tc.value != nil {
			for _, component := range []string{"value", "sampled_at"} {
				v := *tc.value
				if component == "sampled_at" {
					v = 120
				}
				expected = append(expected, map[string]any{"labels": `{addp_metric=` + strconv.Quote(tc.key) + `,addp_component=` + strconv.Quote(component) + `}`, "value": v})
			}
		}
		tests = append(tests, map[string]any{"name": tc.name, "interval": "15s", "input_series": rows, "promql_expr_test": []any{map[string]any{"expr": expression, "eval_time": "2m", "exp_samples": expected}}})
	}
	document, err := json.Marshal(map[string]any{"evaluation_interval": "15s", "tests": tests})
	if err != nil {
		t.Fatal(err)
	}
	runEnginePromQLTests(t, document, project, "redis", "redis-observation")
	t.Logf("Redis fixed PromQL: %d cases passed; owned promtool removed", len(cases))
}

func runEnginePromQLTests(t *testing.T, document []byte, project, fixture, profile string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "rules.json"), document, 0644); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "--profile", profile, "-p", project, "-f", filepath.Join(root, "scripts/test/docker-compose."+fixture+"-t2.yml"), "run", "--rm", "--no-deps", "-v", directory+":/rules:ro", "promtool", "test", "rules", "/rules/rules.json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		lines := []string{}
		for _, line := range strings.Split(string(output), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "expr:") {
				lines = append(lines, line)
			}
		}
		diagnosis := strings.Join(lines, "\n")
		if len(diagnosis) > 8192 {
			diagnosis = diagnosis[:8192]
		}
		t.Fatalf("Prometheus engine proof failed: %v\n%s", err, diagnosis)
	}
}

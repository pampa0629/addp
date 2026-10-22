package iam

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/middleware/requestid"
)

func TestSlowAuthContextDiagnosticThresholdAndSafeFields(t *testing.T) {
	service, output := authContextDiagnosticFixture(t)
	ctx := context.WithValue(t.Context(), requestid.RequestIDKey, "slow-auth-request")
	before, available := service.authContextPoolStats()
	start := time.Now()
	stages := map[string]time.Duration{"transaction_begin": time.Millisecond, "credential_snapshot": 99 * time.Millisecond}
	service.logSlowAuthContextResolution(ctx, start, start.Add(100*time.Millisecond-time.Nanosecond), stages, before, available)
	if output.Len() != 0 {
		t.Fatal("fast successful resolution produced slow diagnostics")
	}
	service.logSlowAuthContextResolution(ctx, start, start.Add(100*time.Millisecond), stages, before, available)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["event"] != "iam.auth_context.resolve_slow" || record["request_id"] != "slow-auth-request" ||
		record["elapsed_ms"] != float64(100) || record["pool_stats_available"] != true {
		t.Fatalf("slow resolution attribution: %#v", record)
	}
	wall := record["stage_wall_ms"].(map[string]any)
	if wall["transaction_begin"] != float64(1) || wall["credential_snapshot"] != float64(99) {
		t.Fatalf("stage timings were not retained: %#v", wall)
	}
	allowed := map[string]bool{}
	for _, key := range []string{"time", "level", "msg", "event", "request_id", "elapsed_ms", "stage_wall_ms", "pool_stats_available",
		"pool_open", "pool_in_use", "pool_idle", "pool_max_open", "pool_wait_count_total", "pool_wait_duration_total_ms",
		"pool_wait_count_delta", "pool_wait_duration_delta_ms"} {
		allowed[key] = true
	}
	for key := range record {
		if !allowed[key] {
			t.Fatalf("unexpected diagnostic field %q", key)
		}
	}
}

func TestSlowAuthContextDiagnosticPoolWaitIsSharedWindowEvidence(t *testing.T) {
	service, output := authContextDiagnosticFixture(t)
	pool, err := service.repository.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	before, available := service.authContextPoolStats()
	held, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, waitErr := pool.Conn(ctx)
	if err := held.Close(); err != nil || waitErr == nil {
		t.Fatalf("expected a bounded pool wait: %v %v", err, waitErr)
	}
	start := time.Now()
	service.logSlowAuthContextResolution(t.Context(), start, start.Add(150*time.Millisecond),
		map[string]time.Duration{"transaction_begin": 150 * time.Millisecond}, before, available)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["pool_wait_count_delta"] != float64(1) || record["pool_wait_duration_delta_ms"].(float64) <= 0 ||
		record["pool_max_open"] != float64(1) {
		t.Fatalf("actual shared pool wait missing: %#v", record)
	}
}

func TestSlowAuthContextDiagnosticUnavailableStatsAreNotZeroWait(t *testing.T) {
	service, output := authContextDiagnosticFixture(t)
	start := time.Now()
	service.logSlowAuthContextResolution(t.Context(), start, start.Add(time.Second),
		map[string]time.Duration{"projection": time.Second}, sql.DBStats{}, false)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["pool_stats_available"] != false {
		t.Fatal("missing pool evidence was counted as available")
	}
	for key := range record {
		if strings.HasPrefix(key, "pool_") && key != "pool_stats_available" {
			t.Fatalf("missing pool evidence was fabricated: %s", key)
		}
	}
}

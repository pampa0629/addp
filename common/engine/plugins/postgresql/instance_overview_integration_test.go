package postgresql

import (
	"context"
	"testing"
)

func TestIntegrationPostgresInstanceOverview(t *testing.T) {
	db, p, info := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	var database string
	if err := db.QueryRowContext(t.Context(), `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	result, err := p.ReadInstanceOverview(t.Context(), info)
	if err != nil || result == nil || len(result.Metrics) != 4 {
		t.Fatalf("overview read failed: %v", err)
	}
	for _, metric := range result.Metrics {
		if metric.Scope != "database" || metric.ScopeName != database || metric.Status != "available" || metric.Value == nil {
			t.Fatalf("invalid native metric %+v", metric)
		}
		if metric.Key == "database_size_bytes" && *metric.Value == 0 {
			t.Fatal("database size was empty")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.ReadInstanceOverview(ctx, info); err == nil {
		t.Fatal("canceled read succeeded")
	}
}

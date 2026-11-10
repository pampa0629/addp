package postgresql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationPostgresSharedSQLPool(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	const engineID = 8410
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	info["pool_max_open_conns"] = 1
	info["pool_max_idle_conns"] = 0
	first := &plugin.Engine{ID: engineID, Version: 1, EngineType: provider.Type(), ConnectionInfo: info}
	pool, err := plugin.GetOrCreatePoolFromFactory(first, nil)
	if err != nil {
		t.Fatal(err)
	}
	native, err := pool.DB()
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	borrowed, err := native.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	run := func(ctx context.Context, version int64, info plugin.ConnectionInfo) (*plugin.QueryResult, error) {
		ctx = plugin.WithPoolEngineVersion(ctx, engineID, version)
		query, err := provider.PrepareQuery(ctx, info, plugin.QueryRequest{
			EngineID: engineID, Language: "sql", Query: "SELECT 42 AS answer",
			Options: plugin.QueryOptions{ReadOnly: true, Limit: 1},
		})
		if err != nil {
			return nil, err
		}
		return query.Execute(ctx)
	}
	blocked, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err := run(blocked, 1, info); !errors.Is(err, context.DeadlineExceeded) ||
		plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeConnectionAcquisitionTimeout || native.Stats().WaitCount != 1 {
		t.Fatalf("bounded query did not wait on the configured pool: %v, stats=%+v", err, native.Stats())
	}
	t.Run("bounded catalog facts", func(t *testing.T) {
		blocked, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancel()
		blocked = plugin.WithPoolEngineVersion(blocked, engineID, 1)
		_, err := provider.DescribeEngineCatalogFacts(blocked, info, plugin.TabularItemPath(engineID, "schema", "public", "pool_sample"), plugin.EngineCatalogFactsOptions{})
		if !errors.Is(err, context.DeadlineExceeded) ||
			plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeConnectionAcquisitionTimeout || native.Stats().WaitCount != 2 {
			t.Fatalf("catalog facts did not wait on the configured pool: %v, stats=%+v", err, native.Stats())
		}
	})
	next := make(plugin.ConnectionInfo, len(info))
	for key, value := range info {
		next[key] = value
	}
	next["pool_max_open_conns"] = 2
	next["pool_max_idle_conns"] = 1
	result, err := run(ctx, 2, next)
	if err != nil || result == nil || len(result.Rows) != 1 || result.Rows[0]["answer"] != int64(42) {
		t.Fatalf("query did not use the replacement pool: %+v, %v", result, err)
	}
	t.Run("bounded native SQL", func(t *testing.T) {
		blocked, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		defer cancel()
		blocked = plugin.WithPoolEngineVersion(blocked, engineID, 2)
		prepared, err := provider.PrepareQuery(blocked, next, plugin.QueryRequest{
			EngineID: engineID, Language: "sql", Query: "SELECT 42 AS answer FROM pg_sleep(2)",
			Options: plugin.QueryOptions{ReadOnly: true, Limit: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := prepared.Execute(blocked); !errors.Is(err, context.DeadlineExceeded) ||
			plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeSQLExecutionTimeout {
			t.Fatalf("native query deadline lost its phase: %v (code %q)", err, plugin.QueryErrorCodeOf(err))
		}
	})
	if err := native.PingContext(ctx); err == nil {
		t.Fatal("old pool still accepts new connections")
	}
	var answer int
	if err := borrowed.QueryRowContext(ctx, "SELECT 42").Scan(&answer); err != nil || answer != 42 {
		t.Fatalf("borrowed PostgreSQL connection did not finish: %d, %v", answer, err)
	}
	if err := borrowed.Close(); err != nil || native.Stats().OpenConnections != 0 {
		t.Fatalf("closed pool retained a physical connection: %+v, %v", native.Stats(), err)
	}
	if _, err := run(ctx, 1, info); !errors.Is(err, plugin.ErrStalePoolConfiguration) {
		t.Fatalf("stale query snapshot was accepted: %v", err)
	}
}

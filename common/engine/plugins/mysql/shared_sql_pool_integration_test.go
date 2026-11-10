package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationMySQLSharedSQLPool(t *testing.T) {
	db, provider, info, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	const engineID = 8411
	defer plugin.ClosePool(engineID)
	ctx := t.Context()
	table := mysqlDialect().QualifiedTable(database, "pool_sample")
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (42)"); err != nil {
		t.Fatal(err)
	}
	info["database"] = database
	info["pool_max_open_conns"] = 1
	info["pool_max_idle_conns"] = 0
	pool, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
		ID: engineID, Version: 1, EngineType: provider.Type(), ConnectionInfo: info,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	native, err := pool.DB()
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := native.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	path := plugin.TabularItemPath(engineID, "database", database, "pool_sample")
	page := func(ctx context.Context, version int64, info plugin.ConnectionInfo) (*plugin.BatchData, error) {
		return provider.ReadBatch(plugin.WithPoolEngineVersion(ctx, engineID, version), info, path,
			plugin.BatchReadOptions{Limit: 1})
	}
	query := func(ctx context.Context, version int64, info plugin.ConnectionInfo) (*plugin.QueryResult, error) {
		ctx = plugin.WithPoolEngineVersion(ctx, engineID, version)
		prepared, err := provider.PrepareQuery(ctx, info, plugin.QueryRequest{
			EngineID: engineID, Language: "sql", Query: "SELECT id FROM " + table,
			Options: plugin.QueryOptions{ReadOnly: true, Limit: 1},
		})
		if err != nil {
			return nil, err
		}
		return prepared.Execute(ctx)
	}
	t.Run("bounded table page", func(t *testing.T) {
		blocked, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancel()
		if _, err := page(blocked, 1, info); !errors.Is(err, context.DeadlineExceeded) ||
			plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeConnectionAcquisitionTimeout || native.Stats().WaitCount != 1 {
			t.Fatalf("bounded page did not wait on the configured pool: %v, stats=%+v", err, native.Stats())
		}
	})
	t.Run("bounded SQL", func(t *testing.T) {
		blocked, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancel()
		if _, err := query(blocked, 1, info); !errors.Is(err, context.DeadlineExceeded) ||
			plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeConnectionAcquisitionTimeout || native.Stats().WaitCount != 2 {
			t.Fatalf("bounded SQL did not wait on the configured pool: %v, stats=%+v", err, native.Stats())
		}
	})
	t.Run("bounded catalog facts", func(t *testing.T) {
		blocked, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancel()
		blocked = plugin.WithPoolEngineVersion(blocked, engineID, 1)
		_, err := provider.DescribeEngineCatalogFacts(blocked, info, path, plugin.EngineCatalogFactsOptions{})
		if !errors.Is(err, context.DeadlineExceeded) ||
			plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeConnectionAcquisitionTimeout || native.Stats().WaitCount != 3 {
			t.Fatalf("catalog facts did not wait on the configured pool: %v, stats=%+v", err, native.Stats())
		}
	})
	next := make(plugin.ConnectionInfo, len(info))
	for key, value := range info {
		next[key] = value
	}
	next["pool_max_open_conns"] = 2
	next["pool_max_idle_conns"] = 1
	batch, err := page(ctx, 2, next)
	if err != nil || batch == nil || len(batch.Rows) != 1 || batch.Rows[0]["id"] != int64(42) {
		t.Fatalf("page did not use the replacement pool: %+v, %v", batch, err)
	}
	result, err := query(ctx, 2, next)
	if err != nil || result == nil || len(result.Rows) != 1 || result.Rows[0]["id"] != int64(42) {
		t.Fatalf("query did not use the replacement pool: %+v, %v", result, err)
	}
	t.Run("catalog facts after pool replacement", func(t *testing.T) {
		ctx := plugin.WithPoolEngineVersion(ctx, engineID, 2)
		facts, err := provider.DescribeEngineCatalogFacts(ctx, next, path, plugin.EngineCatalogFactsOptions{IncludeSpatialFacts: true})
		if err != nil || facts == nil || facts.Table == nil || len(facts.Table.Fields) != 1 ||
			facts.Table.Fields[0].Name != "id" || plugin.EngineCatalogFactsSpatialInfo(facts) != nil {
			t.Fatalf("ordinary MySQL catalog facts failed: %+v, %v", facts, err)
		}
		if _, err := db.ExecContext(ctx, "CREATE TABLE "+mysqlDialect().QualifiedTable(database, "pool_spatial")+" (geom POINT SRID 4326 NOT NULL, SPATIAL INDEX idx_geom (geom)) ENGINE=InnoDB"); err != nil {
			t.Fatal(err)
		}
		path := plugin.TabularItemPath(engineID, "database", database, "pool_spatial")
		facts, err = provider.DescribeEngineCatalogFacts(ctx, next, path, plugin.EngineCatalogFactsOptions{IncludeSpatialFacts: true})
		if err != nil {
			t.Fatal(err)
		}
		spatial := plugin.EngineCatalogFactsSpatialInfo(facts)
		if spatial == nil || spatial.PrimaryCRSRef() != "EPSG:4326" || spatial.IndexName != "idx_geom" {
			t.Fatalf("MySQL catalog lost spatial facts: %+v", spatial)
		}
	})
	t.Run("bounded native SQL", func(t *testing.T) {
		blocked, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		defer cancel()
		blocked = plugin.WithPoolEngineVersion(blocked, engineID, 2)
		prepared, err := provider.PrepareQuery(blocked, next, plugin.QueryRequest{
			EngineID: engineID, Language: "sql", Query: "SELECT SLEEP(2) AS answer",
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
		t.Fatalf("borrowed MySQL connection did not finish: %d, %v", answer, err)
	}
	if err := borrowed.Close(); err != nil || native.Stats().OpenConnections != 0 {
		t.Fatalf("closed pool retained a physical connection: %+v, %v", native.Stats(), err)
	}
	if _, err := page(ctx, 1, info); !errors.Is(err, plugin.ErrStalePoolConfiguration) {
		t.Fatalf("stale page snapshot was accepted: %v", err)
	}
}

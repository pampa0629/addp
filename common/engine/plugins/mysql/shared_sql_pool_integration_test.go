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
	t.Run("source session after client deadline", func(t *testing.T) {
		const probeEngineID = 8412
		defer plugin.ClosePool(probeEngineID)
		probeInfo := make(plugin.ConnectionInfo, len(info))
		for key, value := range info {
			probeInfo[key] = value
		}
		probeInfo["pool_max_idle_conns"] = 1
		probeCtx, stop := context.WithTimeout(t.Context(), 15*time.Second)
		defer stop()
		probeCtx = plugin.WithPoolEngineVersion(probeCtx, probeEngineID, 1)
		lock, err := db.Conn(probeCtx)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		defer lock.ExecContext(context.Background(), "UNLOCK TABLES")
		if _, err := lock.ExecContext(probeCtx, "LOCK TABLES "+table+" WRITE"); err != nil {
			t.Fatal(err)
		}
		request := plugin.QueryRequest{
			EngineID: probeEngineID, Language: "sql",
			Query:   "SELECT id FROM " + table + " /* pool_deadline_probe */",
			Options: plugin.QueryOptions{ReadOnly: true, Limit: 1},
		}
		prepared, err := provider.PrepareQuery(probeCtx, probeInfo, request)
		if err != nil {
			t.Fatal(err)
		}
		clientCtx, cancel := context.WithTimeout(probeCtx, 2*time.Second)
		defer cancel()
		done := make(chan error, 1)
		start := time.Now()
		go func() {
			_, err := prepared.Execute(clientCtx)
			done <- err
		}()
		var sourceID int64
		for sourceID == 0 {
			err = db.QueryRowContext(probeCtx, "SELECT COALESCE(MAX(ID), 0) FROM information_schema.PROCESSLIST WHERE DB = ? AND STATE = 'Waiting for table metadata lock' AND INFO LIKE ?",
				database, "%pool_deadline_probe%").Scan(&sourceID)
			if err != nil {
				t.Fatal(err)
			}
			if sourceID == 0 {
				select {
				case err := <-done:
					t.Fatalf("query ended before native lock observation: %v", err)
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
		err = <-done
		returned := time.Since(start)
		if !errors.Is(err, context.DeadlineExceeded) || plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeSQLExecutionTimeout {
			t.Fatalf("native deadline changed: %v (code %q)", err, plugin.QueryErrorCodeOf(err))
		}
		var retained int
		if err := db.QueryRowContext(probeCtx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID = ? AND DB = ? AND STATE = 'Waiting for table metadata lock'",
			sourceID, database).Scan(&retained); err != nil {
			t.Fatal(err)
		}
		var version string
		if err := db.QueryRowContext(probeCtx, "SELECT VERSION()").Scan(&version); err != nil {
			t.Fatal(err)
		}
		t.Logf("native deadline observation: mysql=%s source_id=%d client_return_ms=%d source_waiter_after_return=%t",
			version, sourceID, returned.Milliseconds(), retained == 1)
		if _, err := lock.ExecContext(probeCtx, "UNLOCK TABLES"); err != nil {
			t.Fatal(err)
		}
		released := time.Now()
		for {
			var count int
			if err := db.QueryRowContext(probeCtx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID = ?", sourceID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count == 0 {
				break
			}
			select {
			case <-probeCtx.Done():
				t.Fatal("source session survived lock release")
			case <-time.After(10 * time.Millisecond):
			}
		}
		prepared, err = provider.PrepareQuery(probeCtx, probeInfo, request)
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := prepared.Execute(probeCtx)
		if err != nil || recovered == nil || len(recovered.Rows) != 1 || recovered.Rows[0]["id"] != int64(42) {
			t.Fatalf("query did not recover after releasing the lock: %+v, %v", recovered, err)
		}
		t.Logf("native deadline cleanup: source_session_gone=true release_to_exit_ms=%d recovered_rows=1",
			time.Since(released).Milliseconds())
	})

}

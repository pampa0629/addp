package mysql

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	mysqldriver "github.com/go-sql-driver/mysql"
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

	t.Run("read-only session timeout admission", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		user := "addp_t2_" + strings.TrimPrefix(database, "addp_common_mysql_it_")
		account := "'" + user + "'@'%'"
		secret := make([]byte, 24)
		if _, err := rand.Read(secret); err != nil {
			t.Fatal(err)
		}
		password := hex.EncodeToString(secret)
		if _, err := db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY '"+password+"'"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := db.ExecContext(context.Background(), "DROP USER IF EXISTS "+account); err != nil {
				t.Errorf("remove owned admission account: %v", err)
			}
		}()
		if _, err := db.ExecContext(ctx, "GRANT SELECT ON "+table+" TO "+account); err != nil {
			t.Fatal(err)
		}
		readerInfo := make(plugin.ConnectionInfo, len(info))
		for key, value := range info {
			readerInfo[key] = value
		}
		readerInfo["user"], readerInfo["password"] = user, password
		dsn, err := provider.BuildDSN(readerInfo)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := sql.Open("mysql", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		reader.SetMaxOpenConns(1)
		reader.SetMaxIdleConns(1)
		conn, err := reader.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var sourceID, originalLock, originalExecution uint64
		if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID(), @@session.lock_wait_timeout, @@session.max_execution_time").Scan(&sourceID, &originalLock, &originalExecution); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, "SET SESSION lock_wait_timeout=1, max_execution_time=0"); err != nil {
			t.Fatalf("SELECT-only account cannot configure session budgets: %v", err)
		}
		defer conn.ExecContext(context.Background(), fmt.Sprintf("SET SESSION lock_wait_timeout=%d, max_execution_time=%d", originalLock, originalExecution))
		lock, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		defer lock.ExecContext(context.Background(), "UNLOCK TABLES")
		if _, err := lock.ExecContext(ctx, "LOCK TABLES "+table+" WRITE"); err != nil {
			t.Fatal(err)
		}
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		var id int
		err = tx.QueryRowContext(ctx, "SELECT id FROM "+table).Scan(&id)
		var nativeError *mysqldriver.MySQLError
		if !errors.As(err, &nativeError) || nativeError.Number != 1205 {
			_ = tx.Rollback()
			t.Fatalf("metadata lock budget returned unexpected error: %v", err)
		}
		lockElapsed := time.Since(started)
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		var waiters int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID=? AND STATE='Waiting for table metadata lock'", sourceID).Scan(&waiters); err != nil || waiters != 0 {
			t.Fatalf("source lock wait survived server timeout: waiters=%d error=%v", waiters, err)
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET SESSION lock_wait_timeout=%d, max_execution_time=100", originalLock)); err != nil {
			t.Fatal(err)
		}
		started = time.Now()
		err = conn.QueryRowContext(ctx, "SELECT id FROM "+table).Scan(&id)
		if !errors.As(err, &nativeError) || nativeError.Number != 3024 {
			t.Fatalf("SELECT budget did not stop metadata lock wait: %v", err)
		}
		selectLockElapsed := time.Since(started)
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID=? AND STATE='Waiting for table metadata lock'", sourceID).Scan(&waiters); err != nil || waiters != 0 {
			t.Fatalf("SELECT source wait survived its server budget: waiters=%d error=%v", waiters, err)
		}
		if _, err := lock.ExecContext(ctx, "UNLOCK TABLES"); err != nil {
			t.Fatal(err)
		}
		started = time.Now()
		err = conn.QueryRowContext(ctx, "WITH RECURSIVE seq AS (SELECT 1 n UNION ALL SELECT n+1 FROM seq WHERE n<1000) SELECT SUM(a.n*b.n*c.n) FROM seq a CROSS JOIN seq b CROSS JOIN seq c").Scan(&id)
		if !errors.As(err, &nativeError) || nativeError.Number != 3024 {
			t.Fatalf("SELECT execution budget returned unexpected error: %v", err)
		}
		executionElapsed := time.Since(started)
		started = time.Now()
		err = conn.QueryRowContext(ctx, "WITH RECURSIVE seq AS (SELECT 1 n UNION ALL SELECT n+1 FROM seq WHERE n<1000) SELECT /*+ MAX_EXECUTION_TIME(300) */ SUM(a.n*b.n*c.n) FROM seq a CROSS JOIN seq b CROSS JOIN seq c").Scan(&id)
		if !errors.As(err, &nativeError) || nativeError.Number != 3024 {
			t.Fatalf("hint execution admission returned unexpected error: %v", err)
		}
		hintElapsed := time.Since(started)
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET SESSION lock_wait_timeout=%d, max_execution_time=%d", originalLock, originalExecution)); err != nil {
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		conn, err = reader.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var reusedID, restoredLock, restoredExecution uint64
		if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID(), @@session.lock_wait_timeout, @@session.max_execution_time").Scan(&reusedID, &restoredLock, &restoredExecution); err != nil {
			t.Fatal(err)
		}
		if reusedID != sourceID || restoredLock != originalLock || restoredExecution != originalExecution {
			t.Fatalf("reused session changed: id=%d/%d lock=%d/%d execution=%d/%d", reusedID, sourceID, restoredLock, originalLock, restoredExecution, originalExecution)
		}
		if err := conn.QueryRowContext(ctx, "SELECT id FROM "+table).Scan(&id); err != nil || id != 42 {
			t.Fatalf("reused read-only session failed: id=%d error=%v", id, err)
		}
		_, err = conn.ExecContext(ctx, "INSERT INTO "+table+" VALUES (99)")
		if !errors.As(err, &nativeError) || nativeError.Number != 1142 {
			t.Fatalf("SELECT-only account write was not denied: %v", err)
		}
		t.Logf("session budget admission: SELECT-only account; source_id=%d lock_timeout_ms=%d code=1205 select_lock_timeout_ms=%d select_execution_timeout_ms=%d hint_timeout_ms=%d code=3024 restored=true reused_same_connection=true recovered_id=42 write_denied=true",
			sourceID, lockElapsed.Milliseconds(), selectLockElapsed.Milliseconds(), executionElapsed.Milliseconds(), hintElapsed.Milliseconds())
	})

}

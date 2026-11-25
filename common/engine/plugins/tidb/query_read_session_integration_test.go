package tidb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationTiDBGuardedQueryReadSession(t *testing.T) {
	if os.Getenv("ADDP_TIDB_INTEGRATION") != "1" {
		t.Skip("set ADDP_TIDB_INTEGRATION=1 to run TiDB integration test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	p := &Plugin{}
	adminInfo := tidbIntegrationConnInfo()
	database := tidbIntegrationEnv("ADDP_TEST_TIDB_DATABASE", "addp_tidb_disposable")
	dsn, err := p.BuildDSN(adminInfo)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	const reader = "'addp_query_read_gate'@'%'"
	const password = "LocalTiDBReadOnly18!"
	if _, err := admin.ExecContext(ctx, "CREATE USER "+reader+" IDENTIFIED BY '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, close := context.WithTimeout(context.Background(), 10*time.Second)
		defer close()
		if _, err := admin.ExecContext(cleanup, "DROP USER "+reader); err != nil {
			t.Error(err)
		}
	}()
	for _, name := range []string{"addp_query_read_first", "addp_query_read_second"} {
		statements := []string{
			"CREATE TABLE " + name + " (id BIGINT PRIMARY KEY, label VARCHAR(30))",
			"INSERT INTO " + name + " VALUES (7, 'authorized result')",
			"GRANT SELECT ON `" + database + "`." + name + " TO " + reader,
		}
		for _, statement := range statements {
			if _, err := admin.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	readerInfo := plugin.ConnectionInfo{}
	for k, v := range adminInfo {
		readerInfo[k] = v
	}
	readerInfo["user"], readerInfo["password"] = "addp_query_read_gate", password
	const engineID = 95002
	first := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, "addp_query_read_first")
	second := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, "addp_query_read_second")
	const single = "SELECT * FROM addp_query_read_first WHERE id=:id"
	denied := errors.New("source authorization refused")
	for _, tc := range []struct {
		name, statement string
		sources         []plugin.EngineCatalogPath
		allowed         bool
	}{
		{"authorized", single, []plugin.EngineCatalogPath{first}, true},
		{"ungranted_join", "SELECT a.id FROM addp_query_read_first a JOIN addp_query_read_second b ON b.id=a.id WHERE a.id=:id", []plugin.EngineCatalogPath{first, second}, false},
		{"withdrawn_same_query", single, []plugin.EngineCatalogPath{first}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := p.PrepareQuery(ctx, readerInfo, plugin.QueryRequest{
				EngineID: engineID, Language: "sql", Query: tc.statement,
				Options: plugin.QueryOptions{ReadOnly: true, Parameters: map[string]any{"id": 7}},
			})
			if err != nil {
				t.Fatal(err)
			}
			want, err := plugin.NewQueryReadSet(tc.sources...)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			var ddlDone chan error
			session, err := p.OpenGuardedQueryReadSession(ctx, prepared, func(_ context.Context, got *plugin.QueryReadSet) error {
				calls++
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("sources=%+v; want=%+v", got, want)
				}
				if tc.allowed {
					ddlDone = make(chan error, 1)
					go func() {
						_, err := admin.ExecContext(ctx, "ALTER TABLE addp_query_read_first ADD COLUMN marker INT")
						ddlDone <- err
					}()
					select {
					case err := <-ddlDone:
						return fmt.Errorf("DDL completed before authorized query opened its cursor: %v", err)
					case <-time.After(2 * time.Second):
						return nil
					}
				}
				return denied
			})
			if session != nil {
				defer session.Close(ctx)
			}
			if !tc.allowed {
				if calls != 1 || session != nil || !errors.Is(err, denied) {
					t.Fatalf("expected denial before business read: calls=%d session=%t err=%v", calls, session != nil, err)
				}
				return
			}
			if err != nil || calls != 1 {
				t.Fatalf("open query calls=%d err=%v", calls, err)
			}
			batch, err := session.ReadBatch(ctx, 2)
			if err != nil || batch == nil || len(batch.Rows) != 1 || batch.Rows[0]["id"] != int64(7) || batch.Rows[0]["label"] != "authorized result" {
				t.Fatalf("rows=%+v err=%v", batch, err)
			}
			if _, exists := batch.Rows[0]["marker"]; exists {
				t.Fatal("query saw a column added after its source was bound")
			}
			if err := session.Close(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-ddlDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("DDL did not complete after releasing source session")
			}
		})
	}
	t.Run("database_scope_native_observations", func(t *testing.T) {
		const outside = "addp_tidb_guard_disposable_other"
		if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+outside); err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := admin.ExecContext(cleanup, "DROP DATABASE "+outside); err != nil {
				t.Error(err)
			}
		}()
		scope := authorization.SourceNodeScope{Target: plugin.TabularNamespacePath(engineID, plugin.EngineCatalogTermDatabase, database)}
		active := true
		authorize := func(_ context.Context, sources *plugin.QueryReadSet) error {
			if !active || len(sources.Paths) == 0 {
				return denied
			}
			for _, path := range sources.Paths {
				if !scope.Covers(path) {
					return denied
				}
			}
			return nil
		}
		// The reserved result alias identifies the user SELECT across these five
		// windows. TiDB's catalog and the Provider's zero-row probes never use it.
		businessSelects := func(t *testing.T) int64 {
			t.Helper()
			var count int64
			err := admin.QueryRowContext(ctx, "SELECT COALESCE(SUM(exec_count),0) FROM information_schema.statements_summary WHERE sample_user=? AND stmt_type='Select' AND LOCATE(?, digest_text)>0", readerInfo["user"], "`node_scope_value`").Scan(&count)
			if err != nil {
				t.Fatal(err)
			}
			return count
		}
		check := func(name, query string, allowed bool, value int64) {
			t.Run(name, func(t *testing.T) {
				before := businessSelects(t)
				prepared, err := p.PrepareQuery(ctx, readerInfo, plugin.QueryRequest{EngineID: engineID, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}})
				if err != nil {
					t.Fatal(err)
				}
				session, err := p.OpenGuardedQueryReadSession(ctx, prepared, authorize)
				if session != nil {
					defer session.Close(ctx)
				}
				var wantReads int64
				if allowed {
					wantReads = 1
					if err != nil || session == nil {
						t.Fatalf("authorized open: %v", err)
					}
					batch, err := session.ReadBatch(ctx, 2)
					if err != nil || batch == nil || len(batch.Rows) != 1 || batch.Rows[0]["node_scope_value"] != value {
						t.Fatalf("business rows=%+v err=%v", batch, err)
					}
					if err := session.Close(ctx); err != nil {
						t.Fatal(err)
					}
				} else if session != nil || !errors.Is(err, denied) {
					t.Fatalf("uncovered table returned a session or unexpected error: %v", err)
				}
				delta := businessSelects(t) - before
				if delta != wantReads {
					t.Fatalf("native business SELECTs=%d, want %d", delta, wantReads)
				}
				t.Logf("native business SELECTs=%d", delta)
			})
		}
		const selectID = "SELECT id AS node_scope_value FROM "
		check("existing", selectID+database+".addp_query_read_first", true, 7)
		for i, dbName := range []string{database, outside} {
			table := dbName + ".node_future"
			for _, sql := range []string{"CREATE TABLE " + table + " (id BIGINT PRIMARY KEY)", fmt.Sprintf("INSERT INTO %s VALUES (%d)", table, 41+i), "GRANT SELECT ON " + table + " TO " + reader} {
				if _, err := admin.ExecContext(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
		}
		future := database + ".node_future"
		check("future", selectID+future, true, 41)
		check("same_name_outside", selectID+outside+".node_future", false, 0)
		check("join_outside", "SELECT a.id AS node_scope_value FROM "+future+" a JOIN "+outside+".node_future b ON a.id=b.id", false, 0)
		active = false
		check("withdrawn", selectID+future, false, 0)
	})
}

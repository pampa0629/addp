package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

func TestIntegrationClickHouseGuardedQueryRead(t *testing.T) {
	if os.Getenv("ADDP_CLICKHOUSE_INTEGRATION") != "1" {
		t.Skip("requires a disposable ClickHouse database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	p := &ClickHousePlugin{}
	info := clickhouseIntegrationConnInfo(t)
	database := plugin.GetString(info, "database")
	adminDSN, err := p.BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("clickhouse", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	for _, stmt := range []string{
		"CREATE TABLE grant_allowed (id UInt64, amount Decimal(20, 2)) ENGINE = MergeTree ORDER BY id",
		"CREATE TABLE grant_other (id UInt64) ENGINE = MergeTree ORDER BY id",
		"CREATE DATABASE guard_other_database",
		"CREATE TABLE guard_other_database.grant_allowed (id UInt64) ENGINE = MergeTree ORDER BY id",
		"CREATE TABLE `Grant.Source` (id UInt64) ENGINE = MergeTree ORDER BY id",
		"CREATE VIEW grant_view AS SELECT id FROM grant_allowed",
		"INSERT INTO grant_allowed VALUES (1, 1.25), (2, 2.50)",
		"INSERT INTO grant_other VALUES (1)",
		"INSERT INTO guard_other_database.grant_allowed VALUES (5)",
		"INSERT INTO `Grant.Source` VALUES (1)",
		"CREATE USER grant_reader IDENTIFIED WITH no_password SETTINGS readonly = 1",
		"GRANT SELECT ON " + database + ".* TO grant_reader",
		"GRANT SELECT ON guard_other_database.grant_allowed TO grant_reader",
	} {
		if _, err := admin.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	info["user"], info["password"] = "grant_reader", ""
	const engineID = 93055
	allowed := plugin.TabularItemPath(engineID, "database", database, "grant_allowed")
	other := plugin.TabularItemPath(engineID, "database", database, "grant_other")
	sameName := plugin.TabularItemPath(engineID, "database", "guard_other_database", "grant_allowed")
	quoted := plugin.TabularItemPath(engineID, "database", database, "Grant.Source")
	authDenied := errors.New("source authorization denied")
	baseSQL := "SELECT id, amount FROM grant_allowed WHERE id = ?"
	for _, tc := range []struct {
		name        string
		query       string
		args        []any
		paths       []plugin.EngineCatalogPath
		deny        error
		wantSelects uint64
	}{
		{"allowed", baseSQL, []any{uint64(1)}, []plugin.EngineCatalogPath{allowed}, nil, 1},
		{"ungranted_join", "SELECT a.id FROM grant_allowed AS a JOIN grant_other AS b ON a.id = b.id", nil, []plugin.EngineCatalogPath{allowed, other}, authDenied, 0},
		{"ungranted_homonym", "SELECT id FROM guard_other_database.grant_allowed", nil, []plugin.EngineCatalogPath{sameName}, authDenied, 0},
		{"revoked", baseSQL, []any{uint64(1)}, []plugin.EngineCatalogPath{allowed}, authDenied, 0},
		{"qualified_quoted_table", "SELECT * FROM " + database + ".`Grant.Source`", nil, []plugin.EngineCatalogPath{quoted}, nil, 1},
		{"cte", "WITH picked AS (SELECT id FROM grant_allowed) SELECT id FROM picked WHERE id = 1", nil, []plugin.EngineCatalogPath{allowed}, nil, 1},
		{"view_unresolved", "SELECT * FROM grant_view", nil, nil, plugin.ErrQueryReadSetUnresolved, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			execCtx := execution.ContextWithLease(ctx, execution.Lease{ExecutionID: id})
			plan, err := p.PrepareQuery(execCtx, info, plugin.QueryRequest{Language: "sql", EngineID: engineID, Query: tc.query, Options: plugin.QueryOptions{ReadOnly: true, Args: tc.args}})
			if err != nil {
				t.Fatal(err)
			}
			checked := false
			session, err := p.OpenGuardedQueryReadSession(execCtx, plan, func(_ context.Context, got *plugin.QueryReadSet) error {
				checked = true
				want, err := plugin.NewQueryReadSet(tc.paths...)
				if err != nil || !reflect.DeepEqual(got.Paths, want.Paths) {
					t.Fatalf("actual sources=%+v want=%+v err=%v", got.Paths, want, err)
				}
				return tc.deny
			})
			if checked != (len(tc.paths) > 0) || !errors.Is(err, tc.deny) {
				t.Fatalf("authorize called=%v, native session error=%v", checked, err)
			}
			if tc.deny == nil {
				batch, err := session.ReadBatch(execCtx, 10)
				if err != nil || len(batch.Rows) != 1 || fmt.Sprint(batch.Rows[0]["id"]) != "1" {
					t.Fatalf("read result=%+v err=%v", batch, err)
				}
				if err := session.Close(execCtx); err != nil {
					t.Fatal(err)
				}
				if err := session.Close(execCtx); err != nil {
					t.Fatalf("repeat close: %v", err)
				}
			} else if session != nil {
				_ = session.Close(execCtx)
				t.Fatal("denied request opened a business cursor")
			}
			if _, err := admin.ExecContext(ctx, "SYSTEM FLUSH LOGS"); err != nil {
				t.Fatal(err)
			}
			var selects uint64
			if err := admin.QueryRowContext(ctx, `
				SELECT count()
				FROM system.query_log
				WHERE query_id = ? AND initial_user = 'grant_reader'
				AND type = 'QueryStart' AND query_kind = 'Select'
			`, id).Scan(&selects); err != nil {
				t.Fatal(err)
			}
			if selects != tc.wantSelects {
				t.Fatalf("business SELECT count=%d want=%d", selects, tc.wantSelects)
			}
			t.Logf("execution_id=%s business_select=%d", id, selects)
		})
	}
}

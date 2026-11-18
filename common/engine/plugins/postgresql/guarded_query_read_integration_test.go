package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func guardedQueryFixture(t *testing.T) (context.Context, *sql.DB, *PostgreSQLPlugin, string, string, func(string) plugin.PreparedQuery) {
	t.Helper()
	db, provider, info := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	name := fmt.Sprintf("guard_query_%d", time.Now().UnixNano())
	qualified := `"common_pg_it"."` + name + `"`
	createPostgresPrepareBaseTable(t, ctx, db, "common_pg_it", name, `id bigint NOT NULL`)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := db.ExecContext(cleanup, `DROP TABLE IF EXISTS `+qualified+` CASCADE`); err != nil {
			t.Error("fixture cleanup", err)
		}
		if _, err := db.ExecContext(cleanup, `DROP FUNCTION IF EXISTS "common_pg_it"."`+name+`_hidden"() CASCADE`); err != nil {
			t.Error("function cleanup", err)
		}
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO `+qualified+` VALUES (101),(202)`); err != nil {
		t.Fatal(err)
	}
	prepare := func(query string) plugin.PreparedQuery {
		t.Helper()
		prepared, err := provider.PrepareQuery(ctx, info, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}})
		if err != nil {
			t.Fatal(err)
		}
		return prepared
	}
	return ctx, db, provider, name, qualified, prepare
}

func TestIntegrationPostgresQueryReadSessionGuardedSources(t *testing.T) {
	ctx, db, provider, name, qualified, prepare := guardedQueryFixture(t)
	if _, err := db.ExecContext(ctx, `CREATE INDEX ON `+qualified+` (id) WHERE id > 0`); err != nil {
		t.Fatal(err)
	}
	t.Run("denial precedes content execution", func(t *testing.T) {
		denied := errors.New("fixture source denied")
		calls := 0
		session, err := provider.OpenGuardedQueryReadSession(ctx, prepare(`SELECT 1/0 FROM `+qualified), func(_ context.Context, set *plugin.QueryReadSet) error {
			calls++
			if len(set.Paths) != 1 || set.Paths[0].Segments[2].Name != name {
				t.Fatal("wrong actual sources", set)
			}
			return denied
		})
		if session != nil {
			_ = session.Close(ctx)
		}
		if !errors.Is(err, denied) || calls != 1 {
			t.Fatalf("read happened before denial: %v calls=%d", err, calls)
		}
		// Denial must release native locks and its connection.
		if _, err := db.ExecContext(ctx, `ALTER TABLE `+qualified+` ADD COLUMN marker integer`); err != nil {
			t.Fatal("denial leaked lock", err)
		}
	})
	t.Run("positive bound source", func(t *testing.T) {
		session, err := provider.OpenGuardedQueryReadSession(ctx, prepare(`SELECT id FROM `+qualified+` ORDER BY id`), func(context.Context, *plugin.QueryReadSet) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(ctx)
		batch, err := session.ReadBatch(ctx, 3)
		if err != nil || len(batch.Rows) != 2 || fmt.Sprint(batch.Rows[0]["id"]) != "101" || fmt.Sprint(batch.Rows[1]["id"]) != "202" {
			t.Fatalf("actual rows=%+v err=%v", batch, err)
		}
	})
	t.Run("join requires both sources before reading", func(t *testing.T) {
		other := name + "_other"
		target := `"common_pg_it"."` + other + `"`
		createPostgresPrepareBaseTable(t, ctx, db, "common_pg_it", other, `id bigint NOT NULL`)
		t.Cleanup(func() {
			clean, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := db.ExecContext(clean, "DROP TABLE "+target); err != nil {
				t.Error(err)
			}
		})
		denied := errors.New("fixture second source denied")
		calls := 0
		session, err := provider.OpenGuardedQueryReadSession(ctx, prepare("WITH a AS (SELECT id FROM "+qualified+") SELECT 1/0 FROM a JOIN "+target+" b ON a.id=b.id"), func(_ context.Context, set *plugin.QueryReadSet) error {
			calls++
			want, err := plugin.NewQueryReadSet(plugin.TabularItemPath(91, "schema", "common_pg_it", name), plugin.TabularItemPath(91, "schema", "common_pg_it", other))
			if err != nil || !reflect.DeepEqual(want, set) {
				t.Fatalf("incorrect business sources: %+v", set)
			}
			return denied
		})
		if session != nil {
			_ = session.Close(ctx)
		}
		if !errors.Is(err, denied) || calls != 1 {
			t.Fatalf("expected joint authorization before execution, got %v, %d calls", err, calls)
		}
	})
}

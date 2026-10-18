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

// Included by the existing QueryReadSession prefix in both the full Common PG
// gate and its query-read-set selection. Source DDL is confined to this fixture.
func TestIntegrationPostgresQueryReadSessionEvidence(t *testing.T) {
	db, pg, conn := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	schema := fmt.Sprintf("session_evidence_%d", time.Now().UnixNano())
	a, b := fmt.Sprintf("session_a_%d", time.Now().UnixNano()), fmt.Sprintf("session_b_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schema, a, `id bigint, label text`)
	createPostgresPrepareBaseTable(t, ctx, db, schema, b, `id bigint, label text`)
	defer func() {
		for _, name := range []string{a, b} {
			if _, err := db.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE "%s"."%s"`, schema, name)); err != nil {
				t.Error(err)
			}
		}
		var remaining int
		if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname=$1 AND tablename IN ($2,$3)`, schema, a, b).Scan(&remaining); err != nil || remaining != 0 {
			t.Errorf("session evidence residual=%d err=%v", remaining, err)
		}
		if _, err := db.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA "%s" RESTRICT`, schema)); err != nil {
			t.Error(err)
		}
	}()
	for _, name := range []string{a, b} {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO "%s"."%s" VALUES (1,'one'),(2,'two')`, schema, name)); err != nil {
			t.Fatal(err)
		}
	}
	path := plugin.TabularItemPath(91, "schema", schema, a)
	for _, mode := range []string{"native", "query"} {
		t.Run(mode, func(t *testing.T) {
			var session plugin.TableReadSession
			var expected *plugin.QueryReadSet
			var err error
			if mode == "native" {
				session, err = pg.OpenTableReadSession(ctx, conn, path, plugin.TableReadSessionOptions{})
				expected, _ = plugin.NewQueryReadSet(path)
			} else {
				prepared, prepareErr := pg.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: fmt.Sprintf(`SELECT a.id FROM "%s"."%s" a JOIN "%s"."%s" b ON a.id=b.id ORDER BY a.id`, schema, a, schema, b), Options: plugin.QueryOptions{ReadOnly: true}})
				if prepareErr != nil {
					t.Fatal(prepareErr)
				}
				session, err = pg.OpenQueryReadSession(ctx, prepared)
				expected, _ = plugin.NewQueryReadSet(path, plugin.TabularItemPath(91, "schema", schema, b))
			}
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(context.Background())
			evidence, ok := session.(plugin.ReadSessionEvidence)
			if !ok {
				t.Fatal("PG session does not expose actual source evidence")
			}
			actual, err := evidence.ReadSet(ctx)
			if err != nil || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("actual session evidence=%+v expected=%+v err=%v", actual, expected, err)
			}
			// No process-local lock: an independent database transaction cannot
			// change the source while this actual cursor is open.
			ddl, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ddl.ExecContext(ctx, "SET LOCAL lock_timeout='60ms'"); err != nil {
				_ = ddl.Rollback()
				t.Fatal(err)
			}
			_, ddlErr := ddl.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s"."%s" RENAME TO "%s_other"`, schema, a, a))
			_ = ddl.Rollback()
			var state interface{ SQLState() string }
			if !errors.As(ddlErr, &state) || state.SQLState() != "55P03" {
				t.Fatalf("actual read session did not protect its source: %v", ddlErr)
			}
			batch, err := session.ReadBatch(ctx, 1)
			if err != nil || len(batch.Rows) != 1 {
				t.Fatalf("cursor proof disturbed read: %+v %v", batch, err)
			}
			if err := session.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if after, err := evidence.ReadSet(ctx); err != nil || !reflect.DeepEqual(after, expected) {
				t.Fatal("closing session lost historical execution evidence", err)
			}
			unlocked, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, lockErr := unlocked.ExecContext(ctx, fmt.Sprintf(`LOCK TABLE "%s"."%s" IN ACCESS EXCLUSIVE MODE NOWAIT`, schema, a))
			_ = unlocked.Rollback()
			if lockErr != nil {
				t.Fatal("session did not release source schema lock", lockErr)
			}
		})
	}
	for _, mode := range []string{"function", "rls"} {
		t.Run("unproven "+mode+" preserves read", func(t *testing.T) {
			query := fmt.Sprintf(`SELECT abs(id) FROM "%s"."%s" ORDER BY id`, schema, a)
			if mode == "rls" {
				if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s"."%s" ENABLE ROW LEVEL SECURITY`, schema, a)); err != nil {
					t.Fatal(err)
				}
				query = fmt.Sprintf(`SELECT id FROM "%s"."%s" ORDER BY id`, schema, a)
			}
			prepared, err := pg.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := pg.OpenQueryReadSession(ctx, prepared)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(context.Background())
			if set, err := session.(plugin.ReadSessionEvidence).ReadSet(ctx); set != nil || !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
				t.Fatal("unproven source was declared complete", set, err)
			}
			if batch, err := session.ReadBatch(ctx, 10); err != nil || len(batch.Rows) != 2 {
				t.Fatal("proof failure poisoned the original read", batch, err)
			}
		})
	}
	t.Run("visible operator over built-in types is not transparent", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION "%[1]s".hidden(bigint,text) RETURNS bigint LANGUAGE sql AS 'SELECT count(*) FROM "%[1]s"."%[2]s"';
 CREATE OPERATOR "%[1]s".+ (LEFTARG=bigint, RIGHTARG=text, FUNCTION="%[1]s".hidden)`, schema, a)); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := db.ExecContext(context.Background(), fmt.Sprintf(`DROP OPERATOR "%[1]s".+ (bigint,text); DROP FUNCTION "%[1]s".hidden(bigint,text)`, schema)); err != nil {
				t.Error(err)
			}
		}()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL search_path=pg_catalog,"%s"`, schema)); err != nil {
			t.Fatal(err)
		}
		query := fmt.Sprintf(`SELECT id + label FROM "%s"."%s"`, schema, b)
		if _, err := tx.ExecContext(ctx, "DECLARE actual_operator_cursor CURSOR FOR "+query); err != nil {
			t.Fatal(err)
		}
		if set := pg.observeSessionReadSet(ctx, tx, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}}); set != nil {
			t.Fatal("user operator's hidden table was omitted from a complete proof")
		}
		var value int64
		if err := tx.QueryRowContext(ctx, "FETCH FORWARD 1 FROM actual_operator_cursor").Scan(&value); err != nil || value != 2 {
			t.Fatal("optional proof altered the bound operator read", value, err)
		}
	})
}

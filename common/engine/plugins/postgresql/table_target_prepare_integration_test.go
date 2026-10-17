package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
)

func TestIntegrationPostgresPrepareTableWriteTargetOutcomes(t *testing.T) {
	db, pg, info := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	schema := fmt.Sprintf("addp_target_prepare_%d", time.Now().UnixNano())
	qualified := func(name string) string { return pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(name) }
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		for _, name := range []string{"target", "race", "invalid"} {
			if _, err := db.ExecContext(cleanup, "DROP TABLE IF EXISTS "+qualified(name)+" RESTRICT"); err != nil {
				t.Errorf("cleanup target %s: %v", name, err)
			}
		}
		if _, err := db.ExecContext(cleanup, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" RESTRICT"); err != nil {
			t.Errorf("cleanup target schema: %v", err)
		}
		var remains bool
		if err := db.QueryRowContext(cleanup, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&remains); err != nil || remains {
			t.Errorf("target cleanup not proven: remaining=%v error=%v", remains, err)
		}
	})
	options := plugin.TableTargetPrepareOptions{Definition: plugin.TableWriteOptions{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}}, RecreateExisting: true}
	path := postgresPrepareTablePath(schema, "target")
	result, err := pg.PrepareTableTarget(ctx, info, path, options)
	if err != nil || result.Disposition != plugin.TableTargetCreated {
		t.Fatalf("first prepare=%+v error=%v", result, err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+qualified("target")+" VALUES (7)"); err != nil {
		t.Fatal(err)
	}
	var originalOID int64
	if err := db.QueryRowContext(ctx, "SELECT $1::regclass::oid", qualified("target")).Scan(&originalOID); err != nil {
		t.Fatal(err)
	}
	options.RecreateExisting = false
	result, err = pg.PrepareTableTarget(ctx, info, path, options)
	var retained int
	if readErr := db.QueryRowContext(ctx, "SELECT id FROM "+qualified("target")).Scan(&retained); err != nil || readErr != nil || result.Disposition != plugin.TableTargetReused || retained != 7 {
		t.Fatalf("reuse=%+v error=%v read=%v value=%d", result, err, readErr, retained)
	}
	options.RecreateExisting = true
	options.Definition.Fields = []datatype.FieldInfo{{Name: "label", Type: datatype.FieldTypeString}}
	result, err = pg.PrepareTableTarget(ctx, info, path, options)
	if err != nil || result.Disposition != plugin.TableTargetRecreated {
		t.Fatalf("recreate=%+v error=%v", result, err)
	}
	var nextOID int64
	if err := db.QueryRowContext(ctx, "SELECT $1::regclass::oid", qualified("target")).Scan(&nextOID); err != nil || nextOID == originalOID {
		t.Fatalf("replace did not rebuild: old=%d new=%d error=%v", originalOID, nextOID, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(label) FROM "+qualified("target")).Scan(&count); err != nil || count != 0 {
		t.Fatalf("new target shape/rows: count=%d error=%v", count, err)
	}

	t.Run("concurrent creator is not dropped or adopted", func(t *testing.T) {
		// Freeze an absent observation, then let a separate connection win the
		// name before the production helper executes the creation phase.
		var absent bool
		if err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NULL", qualified("race")).Scan(&absent); err != nil || !absent {
			t.Fatalf("race baseline absent=%v error=%v", absent, err)
		}
		dsn, err := pg.BuildDSN(info)
		if err != nil {
			t.Fatal(err)
		}
		peer, err := sql.Open(pg.sqlDriverName(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer peer.Close()
		if _, err := peer.ExecContext(ctx, "CREATE TABLE "+qualified("race")+" (sentinel bigint); INSERT INTO "+qualified("race")+" VALUES (99)"); err != nil {
			t.Fatal(err)
		}
		result, err := prepareObservedPostgresTableTarget(ctx, db, schema, "race", options, false)
		var pgErr *pq.Error
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "42P07" || result != (plugin.TableTargetPreparation{}) {
			t.Fatalf("race was not refused: result=%+v error=%v", result, err)
		}
		var sentinel int
		if err := peer.QueryRowContext(ctx, "SELECT sentinel FROM "+qualified("race")).Scan(&sentinel); err != nil || sentinel != 99 {
			t.Fatalf("concurrent target changed: sentinel=%d error=%v", sentinel, err)
		}
	})

	t.Run("invalid definition has no delete effects", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified("invalid")+" (id bigint); INSERT INTO "+qualified("invalid")+" VALUES (23)"); err != nil {
			t.Fatal(err)
		}
		invalid := options
		invalid.Definition.Fields = []datatype.FieldInfo{{Name: "members", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON}}
		result, err := pg.PrepareTableTarget(ctx, info, postgresPrepareTablePath(schema, "invalid"), invalid)
		if err == nil || result != (plugin.TableTargetPreparation{}) {
			t.Fatalf("invalid=%+v error=%v", result, err)
		}
		var sentinel int
		if err := db.QueryRowContext(ctx, "SELECT id FROM "+qualified("invalid")).Scan(&sentinel); err != nil || sentinel != 23 {
			t.Fatalf("invalid definition deleted target: value=%d error=%v", sentinel, err)
		}
	})
}

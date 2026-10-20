package postgresql

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
)

// The prefix is selected by the existing full Common PostgreSQL T2 gate.
func TestIntegrationPostgresPrepareTableWriteCreationWitness(t *testing.T) {
	db, pg, info := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	schema := fmt.Sprintf("addp_creation_witness_%d", time.Now().UnixNano())
	qualified := func(table string) string { return pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(table) }
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	cases := []string{"commit", "namespace_rename", "empty", "abort", "batch_error", "disconnect", "cancel", "replaced", "reused", "recreated", "catalog_changed", "missing_witness", "wrong_engine", "wrong_target"}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		for _, name := range append(cases, "other_target") {
			if _, err := db.ExecContext(cleanup, "DROP TABLE IF EXISTS "+qualified(name)+" RESTRICT"); err != nil {
				t.Errorf("cleanup creation witness table %s: %v", name, err)
			}
		}
		if _, err := db.ExecContext(cleanup, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" RESTRICT"); err != nil {
			t.Errorf("cleanup creation witness schema: %v", err)
		}
		var remains bool
		if err := db.QueryRowContext(cleanup, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)", schema).Scan(&remains); err != nil || remains {
			t.Errorf("creation witness cleanup not proven: remains=%v error=%v", remains, err)
		}
	})
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}
	for _, scenario := range cases {
		t.Run(scenario, func(t *testing.T) {
			path := postgresPrepareTablePath(schema, scenario)
			path.EngineID = 2
			if scenario == "reused" || scenario == "recreated" {
				if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified(scenario)+" (id bigint); INSERT INTO "+qualified(scenario)+" VALUES (99)"); err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := pg.PrepareTableTarget(ctx, info, path, plugin.TableTargetPrepareOptions{Definition: plugin.TableWriteOptions{Fields: fields}, RecreateExisting: scenario == "recreated"})
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "reused" && scenario != "recreated" && decodePostgresCreationWitness(prepared.CreationWitness) == nil {
				t.Fatal("strict CREATE did not produce its same-transaction witness")
			}
			if scenario == "replaced" {
				if _, err := db.ExecContext(ctx, "DROP TABLE "+qualified(scenario)+"; CREATE TABLE "+qualified(scenario)+" (id bigint); INSERT INTO "+qualified(scenario)+" VALUES (99)"); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "catalog_changed" {
				if _, err := db.ExecContext(ctx, "ALTER TABLE "+qualified(scenario)+" ADD COLUMN extra text"); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "missing_witness" {
				prepared.CreationWitness = ""
			}
			if scenario == "wrong_engine" {
				path.EngineID++
			}
			if scenario == "wrong_target" {
				if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified("other_target")+" (id bigint)"); err != nil {
					t.Fatal(err)
				}
				path = postgresPrepareTablePath(schema, "other_target")
				path.EngineID = 2
			}
			writeCtx, stopWrite := context.WithCancel(ctx)
			defer stopWrite()
			session, err := pg.OpenTableWriteSession(writeCtx, info, path, plugin.TableWriteSessionOptions{Method: "copy", Fields: fields, TargetPreparation: prepared})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Abort(context.Background())
			evidence := session.(plugin.CreatedTableCommitEvidenceProvider)
			if evidence.CreatedTableCommitted() {
				t.Fatal("open session became commit evidence")
			}
			if scenario == "namespace_rename" {
				// Maintenance during a task is outside the operating contract.
				// Prove detection, not an unsupported parent-namespace lock.
				if _, err := db.ExecContext(ctx, "ALTER SCHEMA "+pq.QuoteIdentifier(schema)+" RENAME TO "+pq.QuoteIdentifier(schema+"_moved")); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := db.ExecContext(context.Background(), "ALTER SCHEMA "+pq.QuoteIdentifier(schema+"_moved")+" RENAME TO "+pq.QuoteIdentifier(schema)); err != nil {
						t.Errorf("restore owned namespace: %v", err)
					}
				}()
			}
			if scenario == "commit" {
				peer, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer peer.Rollback()
				if _, err := peer.ExecContext(ctx, "SET LOCAL lock_timeout='500ms'"); err != nil {
					t.Fatal(err)
				}
				_, err = peer.ExecContext(ctx, "DROP TABLE "+qualified(scenario))
				var pgErr *pq.Error
				if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
					t.Fatalf("real write transaction did not protect its relation: %v", err)
				}
				_ = peer.Rollback()
			}
			if scenario == "disconnect" {
				var pid int
				// This test alone owns the exact random-schema relation. Resolve
				// and terminate only the COPY backend holding that relation lock.
				if err := db.QueryRowContext(ctx, `SELECT a.pid FROM pg_catalog.pg_stat_activity a
 JOIN pg_catalog.pg_locks l ON l.pid=a.pid
 WHERE a.datname=current_database() AND a.usename=current_user AND a.pid<>pg_backend_pid()
 AND l.database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())
 AND l.relation=$1::regclass AND l.mode='RowExclusiveLock' AND l.granted`, qualified(scenario)).Scan(&pid); err != nil {
					t.Fatal(err)
				}
				var killed bool
				if err := db.QueryRowContext(ctx, `SELECT pg_terminate_backend(a.pid) FROM pg_catalog.pg_stat_activity a
 WHERE a.pid=$1 AND a.datname=current_database() AND a.usename=current_user
 AND EXISTS (SELECT 1 FROM pg_catalog.pg_locks l WHERE l.pid=a.pid AND l.relation=$2::regclass AND l.mode='RowExclusiveLock' AND l.granted)`, pid, qualified(scenario)).Scan(&killed); err != nil || !killed {
					t.Fatalf("terminate exact owned writer: killed=%v error=%v", killed, err)
				}
			}
			if scenario == "cancel" {
				stopWrite()
			}
			if scenario == "abort" {
				if err := session.Abort(ctx); err != nil {
					t.Fatal(err)
				}
				if evidence.CreatedTableCommitted() {
					t.Fatal("Abort became successful evidence")
				}
				return
			}
			if scenario != "empty" {
				var value any = int64(9)
				if scenario == "batch_error" {
					value = "not an integer"
				}
				writeErr := session.WriteBatch(writeCtx, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"id": value}}})
				// COPY may buffer a server-side type error until Close flushes
				// the stream. Only successful commit can establish evidence.
				if writeErr != nil && scenario != "batch_error" && scenario != "disconnect" && scenario != "cancel" {
					t.Fatal(writeErr)
				}
			}
			if evidence.CreatedTableCommitted() {
				t.Fatal("uncommitted rows became evidence")
			}
			closeErr := session.Close(writeCtx)
			failure := scenario == "batch_error" || scenario == "disconnect" || scenario == "cancel"
			if (closeErr != nil) != failure {
				t.Fatalf("close error=%v failure=%v", closeErr, failure)
			}
			wantEvidence := scenario == "commit" || scenario == "empty"
			if got := evidence.CreatedTableCommitted(); got != wantEvidence {
				t.Fatalf("created+committed=%v want=%v", got, wantEvidence)
			}
			if failure {
				if session.Close(writeCtx) == nil || evidence.CreatedTableCommitted() {
					t.Fatal("retry Close revived failed commit")
				}
			} else if err := session.Close(writeCtx); err != nil || evidence.CreatedTableCommitted() != wantEvidence {
				t.Fatal("repeat Close changed immutable commit result", err)
			}
			if scenario == "replaced" || scenario == "reused" {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+qualified(scenario)+" WHERE id IN (9,99)").Scan(&count); err != nil || count != 2 {
					t.Fatalf("unproven evidence changed original append strategy: count=%d error=%v", count, err)
				}
			}
			// Once committed, later physical replacement cannot erase this past
			// execution fact; it is not a current-object ACL or access decision.
			if scenario == "commit" {
				if _, err := db.ExecContext(ctx, "DROP TABLE "+qualified(scenario)+"; CREATE TABLE "+qualified(scenario)+" (id bigint)"); err != nil {
					t.Fatal(err)
				}
				if !evidence.CreatedTableCommitted() {
					t.Fatal("later replacement rewrote committed execution fact")
				}
			}
		})
	}
}

var _ plugin.CreatedTableCommitEvidenceProvider = (*postgresTableWriteSession)(nil)

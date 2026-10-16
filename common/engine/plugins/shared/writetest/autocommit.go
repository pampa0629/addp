package writetest

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	_ "github.com/mattn/go-sqlite3"
)

// AutoCommitSQLite opens independent writer/observer handles. It checks session
// state and binding only; the same assertions run against real engines in T2.
func AutoCommitSQLite(t *testing.T) (writer, observer *sql.DB, qualified string) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "autocommit.db")
	var err error
	writer, err = sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	observer, err = sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = observer.Close() })
	if _, err := writer.Exec(`CREATE TABLE target (id INTEGER, document TEXT, payload BLOB, label TEXT CHECK (length(label) <= 8))`); err != nil {
		t.Fatal(err)
	}
	return writer, observer, "main.target"
}

// AutoCommitSessionFailures verifies that failed sessions cannot advertise a
// successful session commit. Already committed batches/chunks remain visible.
func AutoCommitSessionFailures(t *testing.T, setup func(*testing.T) (plugin.TableWriteSession, *sql.DB, string)) {
	t.Helper()
	for _, scenario := range []string{"invalid_json", "late_execute_failure", "canceled_write", "canceled_close", "abort", "success", "empty_success"} {
		t.Run(scenario, func(t *testing.T) {
			session, observer, qualified := setup(t)
			t.Cleanup(func() { _ = session.Abort(context.Background()) })
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			row := func(id int64) map[string]interface{} {
				return map[string]interface{}{"id": id, "document": map[string]interface{}{"ok": true}, "payload": []byte{0, 255, 65}, "label": "plain {"}
			}
			wantRows := int64(0)
			if scenario != "empty_success" {
				if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{row(1)}}); err != nil {
					t.Fatal(err)
				}
				wantRows = 1
			}
			closeCtx := ctx
			var firstErr error
			switch scenario {
			case "invalid_json", "late_execute_failure":
				rows := make([]map[string]interface{}, 1001)
				for i := range rows {
					rows[i] = row(int64(i + 2))
				}
				if scenario == "invalid_json" {
					rows[1000]["document"] = "{"
				} else {
					// The final row violates a fixture constraint in the real
					// database after the first chunk has committed.
					rows[1000]["label"] = "synthetic constraint failure"
					wantRows += 1000
				}
				firstErr = session.WriteBatch(ctx, &plugin.BatchData{Rows: rows})
				if firstErr == nil {
					t.Fatal("expected write failure")
				}
				if scenario == "late_execute_failure" && !strings.Contains(firstErr.Error(), "rows 1000-1001") {
					t.Fatalf("failure did not reach later chunk: %v", firstErr)
				}
			case "canceled_write", "canceled_close":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				if scenario == "canceled_close" {
					closeCtx, firstErr = canceled, context.Canceled
				} else {
					firstErr = session.WriteBatch(canceled, &plugin.BatchData{Rows: []map[string]interface{}{row(2)}})
					if !errors.Is(firstErr, context.Canceled) {
						t.Fatalf("cancellation cause lost: %v", firstErr)
					}
				}
			case "success":
				if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{row(2)}}); err != nil {
					t.Fatal(err)
				}
				wantRows++
			case "abort":
				if err := session.Abort(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if firstErr != nil && scenario != "canceled_close" {
				for _, batch := range []*plugin.BatchData{nil, {Rows: []map[string]interface{}{row(5000)}}} {
					if err := session.WriteBatch(ctx, batch); !errors.Is(err, firstErr) {
						t.Errorf("subsequent write lost first error: got=%v first=%v", err, firstErr)
					}
				}
			}
			for _, closeCtx := range []context.Context{closeCtx, ctx} {
				err := session.Close(closeCtx)
				if firstErr != nil && !errors.Is(err, firstErr) || firstErr == nil && err != nil {
					t.Errorf("Close result: got=%v first=%v", err, firstErr)
				}
			}
			marker := session.(plugin.CommitMarkerProvider).CommitMarker()
			if firstErr != nil || scenario == "abort" {
				if marker != nil {
					t.Error("failed/aborted session published success marker")
				}
			} else if marker == nil || marker.CommitPosition["rows_committed"] != wantRows || marker.CommitPosition["batches_committed"] != wantRows {
				t.Errorf("successful session marker=%+v want rows/batches=%d", marker, wantRows)
			}
			var actual int64
			if err := observer.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&actual); err != nil || actual != wantRows {
				t.Fatalf("committed rows=%d want=%d err=%v", actual, wantRows, err)
			}
			if wantRows > 0 {
				var document, label string
				var payload []byte
				if err := observer.QueryRowContext(ctx, "SELECT document,payload,label FROM "+qualified+" WHERE id=1").Scan(&document, &payload, &label); err != nil || document != `{"ok":true}` || !bytes.Equal(payload, []byte{0, 255, 65}) || label != "plain {" {
					t.Fatalf("prior committed JSON/bytes changed: doc=%q payload=%v label=%q err=%v", document, payload, label, err)
				}
			}
		})
	}
}

package postgresql

import (
	"context"
	"database/sql"
	"errors"
	_ "github.com/mattn/go-sqlite3"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

func TestPostgresOpenTableWriteSessionRejectsResumeMarker(t *testing.T) {
	postgresPlugin := &PostgreSQLPlugin{}
	_, err := postgresPlugin.OpenTableWriteSession(nil, nil, plugin.EngineCatalogPath{}, plugin.TableWriteSessionOptions{
		ResumeMarker: &resume.Marker{Version: resume.MarkerVersionV1},
	})
	if err == nil {
		t.Fatal("OpenTableWriteSession succeeded with resume marker, want explicit unsupported error")
	}
}

func TestPostgresTableWriteSessionCommitMarkerIsNilBeforeCommit(t *testing.T) {
	session := &postgresTableWriteSession{
		schema:         "public",
		table:          "roads",
		columns:        []string{"id", "name"},
		batchesWritten: 1,
		rowsWritten:    2,
	}

	if marker := session.CommitMarker(); marker != nil {
		t.Fatalf("CommitMarker() = %#v, want nil before commit", marker)
	}
}

func TestPostgresTableWriteSessionBuildCommitMarker(t *testing.T) {
	session := &postgresTableWriteSession{
		schema:         "public",
		table:          "roads",
		columns:        []string{"id", "name"},
		batchesWritten: 2,
		rowsWritten:    3,
	}
	session.commitMarker = session.buildCommitMarker()

	marker := session.CommitMarker()
	if marker == nil {
		t.Fatal("CommitMarker() = nil, want marker after commit")
	}
	if marker.Version != resume.MarkerVersionV1 ||
		marker.Provider != "postgresql.table_write_session" ||
		marker.PositionUnit != "session_commit" {
		t.Fatalf("marker identity = %#v, want postgresql session commit marker", marker)
	}
	if marker.CommitPosition["rows_committed"] != int64(3) ||
		marker.CommitPosition["batches_committed"] != int64(2) {
		t.Fatalf("commit position = %#v, want committed rows and batches", marker.CommitPosition)
	}
	if marker.Fingerprint["target"] != "public/roads" ||
		marker.Fingerprint["schema"] != "public" ||
		marker.Fingerprint["table"] != "roads" ||
		marker.Fingerprint["method"] != "postgres_copy" {
		t.Fatalf("fingerprint = %#v, want target facts", marker.Fingerprint)
	}
	columns, ok := marker.Fingerprint["columns"].([]string)
	if !ok || len(columns) != 2 || columns[0] != "id" || columns[1] != "name" {
		t.Fatalf("columns fingerprint = %#v, want copied column list", marker.Fingerprint["columns"])
	}

	columns[0] = "mutated"
	fresh := session.CommitMarker()
	freshColumns := fresh.Fingerprint["columns"].([]string)
	if freshColumns[0] != "id" {
		t.Fatalf("CommitMarker() exposed mutable columns: %#v", freshColumns)
	}
}

// T1 checks the shared close state machine; native COPY is covered by typed-write T2.
func TestPostgresTableWriteSessionRetainsCloseFailure(t *testing.T) {
	for _, failure := range []string{"closed_statement", "canceled_close"} {
		t.Run(failure, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			stmt, err := tx.PrepareContext(t.Context(), "SELECT 1")
			if err != nil {
				t.Fatal(err)
			}
			session := &postgresTableWriteSession{db: db, tx: tx, stmt: stmt}
			ctx := t.Context()
			if failure == "closed_statement" {
				if err := stmt.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			first := session.Close(ctx)
			var typed *plugin.TableWriteError
			if !errors.As(first, &typed) || typed.Code != plugin.TableWriteFailed || typed.Field != nil {
				t.Fatalf("close diagnostic: %#v error=%v", typed, first)
			}
			if failure == "canceled_close" && !errors.Is(first, context.Canceled) {
				t.Fatalf("cancel cause lost: %v", first)
			}
			for i := 0; i < 2; i++ {
				if err := session.Close(t.Context()); !errors.Is(err, first) {
					t.Fatalf("repeated Close lost failure: got=%v first=%v", err, first)
				}
			}
			if session.CommitMarker() != nil {
				t.Fatal("failed close published marker")
			}
		})
	}
}

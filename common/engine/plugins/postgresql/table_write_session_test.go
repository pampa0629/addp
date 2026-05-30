package postgresql

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

func TestPostgresOpenTableWriteSessionRejectsResumeMarker(t *testing.T) {
	postgresPlugin := &PostgreSQLPlugin{}
	_, err := postgresPlugin.OpenTableWriteSession(nil, nil, plugin.CatalogPath{}, plugin.TableWriteSessionOptions{
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

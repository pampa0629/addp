package mysql

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

func TestMySQLFieldColumnsUsesFieldOrder(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "id"},
		{Name: "name"},
		{Name: "id"},
		{Name: "  "},
	}

	got := mysqlFieldColumns(fields)
	want := []string{"id", "name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mysqlFieldColumns() = %#v, want %#v", got, want)
	}
}

func TestMySQLBatchColumnsFallsBackToSortedRowKeys(t *testing.T) {
	batch := &plugin.BatchData{
		Rows: []map[string]interface{}{
			{"name": "Ada", "id": 1},
			{"created_at": "2026-05-30"},
		},
	}

	got := mysqlBatchColumns(batch)
	want := []string{"created_at", "id", "name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mysqlBatchColumns() = %#v, want %#v", got, want)
	}
}

func TestEffectiveMySQLInsertChunkSizeCapsBindParams(t *testing.T) {
	if got := effectiveMySQLInsertChunkSize(100, 1000); got != 655 {
		t.Fatalf("effectiveMySQLInsertChunkSize() = %d, want 655", got)
	}
	if got := effectiveMySQLInsertChunkSize(70000, 1000); got != 0 {
		t.Fatalf("effectiveMySQLInsertChunkSize() = %d, want 0 for impossible column count", got)
	}
	if got := effectiveMySQLInsertChunkSize(2, 10); got != 10 {
		t.Fatalf("effectiveMySQLInsertChunkSize() = %d, want requested size", got)
	}
}

func TestBuildMySQLInsertSQL(t *testing.T) {
	rows := []map[string]interface{}{
		{"id": 1, "city`name": "Hangzhou"},
		{"id": 2, "city`name": "Shanghai"},
	}

	sql, args := buildMySQLInsertSQL("analytics", "target table", []string{"id", "city`name"}, rows)
	wantSQL := "INSERT INTO `analytics`.`target table` (`id`, `city``name`) VALUES (?, ?), (?, ?)"
	if sql != wantSQL {
		t.Fatalf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []interface{}{1, "Hangzhou", 2, "Shanghai"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestShouldUseMySQLInsertWriteMethod(t *testing.T) {
	for _, method := range []string{"", "insert", "mysql_insert", "copy"} {
		if !shouldUseMySQLInsertWriteMethod(method) {
			t.Fatalf("shouldUseMySQLInsertWriteMethod(%q) = false, want true", method)
		}
	}
	if shouldUseMySQLInsertWriteMethod("postgres_copy") {
		t.Fatal("shouldUseMySQLInsertWriteMethod(postgres_copy) = true, want false")
	}
}

func TestMySQLOpenTableWriteSessionRejectsResumeMarker(t *testing.T) {
	mysqlPlugin := &MySQLPlugin{}
	_, err := mysqlPlugin.OpenTableWriteSession(nil, nil, plugin.CatalogPath{}, plugin.TableWriteSessionOptions{
		ResumeMarker: &resume.Marker{Version: resume.MarkerVersionV1},
	})
	if err == nil {
		t.Fatal("OpenTableWriteSession succeeded with resume marker, want explicit unsupported error")
	}
}

func TestMySQLTableWriteSessionCommitMarkerIsNilBeforeCommit(t *testing.T) {
	session := &mysqlTableWriteSession{
		database:       "analytics",
		table:          "events",
		columns:        []string{"id", "name"},
		batchesWritten: 1,
		rowsWritten:    2,
	}

	if marker := session.CommitMarker(); marker != nil {
		t.Fatalf("CommitMarker() = %#v, want nil before commit", marker)
	}
}

func TestMySQLTableWriteSessionBuildCommitMarker(t *testing.T) {
	session := &mysqlTableWriteSession{
		database:       "analytics",
		table:          "events",
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
		marker.Provider != "mysql.table_write_session" ||
		marker.PositionUnit != "session_commit" {
		t.Fatalf("marker identity = %#v, want mysql session commit marker", marker)
	}
	if marker.CommitPosition["rows_committed"] != int64(3) ||
		marker.CommitPosition["batches_committed"] != int64(2) {
		t.Fatalf("commit position = %#v, want committed rows and batches", marker.CommitPosition)
	}
	if marker.Fingerprint["target"] != "analytics/events" ||
		marker.Fingerprint["database"] != "analytics" ||
		marker.Fingerprint["table"] != "events" ||
		marker.Fingerprint["method"] != "mysql_insert" {
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

func TestMySQLTablePathPartsErrorText(t *testing.T) {
	_, _, err := mysqlTablePathParts(plugin.CatalogPath{})
	if err == nil || !strings.Contains(err.Error(), "database/table") {
		t.Fatalf("mysqlTablePathParts error = %v, want database/table", err)
	}
}

package postgresql

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestBatchColumnsUsesFieldOrder(t *testing.T) {
	batch := &plugin.BatchData{
		Fields: []datatype.FieldInfo{
			{Name: "id"},
			{Name: "name"},
			{Name: "id"},
			{Name: "  "},
		},
		Rows: []map[string]interface{}{{"name": "Ada", "id": 1}},
	}

	got := batchColumns(batch)
	want := []string{"id", "name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batchColumns() = %#v, want %#v", got, want)
	}
}

func TestBatchColumnsFallsBackToSortedRowKeys(t *testing.T) {
	batch := &plugin.BatchData{
		Rows: []map[string]interface{}{
			{"name": "Ada", "id": 1},
			{"created_at": "2026-05-16"},
		},
	}

	got := batchColumns(batch)
	want := []string{"created_at", "id", "name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batchColumns() = %#v, want %#v", got, want)
	}
}

func TestEffectivePostgresInsertChunkSizeCapsBindParams(t *testing.T) {
	if got := effectivePostgresInsertChunkSize(100, 1000); got != 655 {
		t.Fatalf("effectivePostgresInsertChunkSize() = %d, want 655", got)
	}
	if got := effectivePostgresInsertChunkSize(70000, 1000); got != 0 {
		t.Fatalf("effectivePostgresInsertChunkSize() = %d, want 0 for impossible column count", got)
	}
	if got := effectivePostgresInsertChunkSize(2, 10); got != 10 {
		t.Fatalf("effectivePostgresInsertChunkSize() = %d, want requested size", got)
	}
}

func TestBuildPostgresInsertSQL(t *testing.T) {
	rows := []map[string]interface{}{
		{"id": 1, `city"name`: "Hangzhou"},
		{"id": 2, `city"name`: "Shanghai"},
	}

	sql, args := buildPostgresInsertSQL("public", "target table", []string{"id", `city"name`}, rows, nil)
	wantSQL := `INSERT INTO "public"."target table" ("id", "city""name") VALUES ($1, $2), ($3, $4)`
	if sql != wantSQL {
		t.Fatalf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []interface{}{1, "Hangzhou", 2, "Shanghai"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestBuildPostgresInsertSQLNormalizesGeometryBytes(t *testing.T) {
	rows := []map[string]interface{}{
		{"id": 1, "geom": []byte{0x01, 0x02, 0x0f}},
	}
	geometryColumns := map[string]struct{}{"geom": {}}

	sql, args := buildPostgresInsertSQL("public", "roads", []string{"id", "geom"}, rows, geometryColumns)
	wantSQL := `INSERT INTO "public"."roads" ("id", "geom") VALUES ($1, $2)`
	if sql != wantSQL {
		t.Fatalf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []interface{}{1, "01020f"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestPostgresGeometryColumns(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "id", Type: "int"},
		{Name: "geom", Type: "geometry"},
		{Name: "shape", Type: "polygon"},
	}

	got := postgresGeometryColumns(fields)
	if _, ok := got["geom"]; !ok {
		t.Fatal("geom was not detected as geometry")
	}
	if _, ok := got["shape"]; !ok {
		t.Fatal("shape was not detected as geometry")
	}
	if _, ok := got["id"]; ok {
		t.Fatal("id was detected as geometry")
	}
}

func TestTablePathPartsRequiresSchemaAndTable(t *testing.T) {
	_, _, err := tablePathParts(plugin.CatalogPath{})
	if err == nil {
		t.Fatal("tablePathParts() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "schema/table") {
		t.Fatalf("error = %q, want schema/table", err)
	}
}

func TestShouldUseCopyBatchWrite(t *testing.T) {
	if !shouldUseCopyBatchWrite(plugin.BatchWriteOptions{}, nil) {
		t.Fatal("shouldUseCopyBatchWrite(empty) = false, want writer default copy")
	}
	if !shouldUseCopyBatchWrite(plugin.BatchWriteOptions{Method: "copy"}, nil) {
		t.Fatal("shouldUseCopyBatchWrite(copy) = false, want true")
	}
	if !shouldUseCopyBatchWrite(plugin.BatchWriteOptions{}, &plugin.BatchData{Hints: map[string]interface{}{"write_method": "postgres_copy"}}) {
		t.Fatal("shouldUseCopyBatchWrite(hint postgres_copy) = false, want true")
	}
	if shouldUseCopyBatchWrite(plugin.BatchWriteOptions{Method: "insert"}, nil) {
		t.Fatal("shouldUseCopyBatchWrite(insert) = true, want false")
	}
}

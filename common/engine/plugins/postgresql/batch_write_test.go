package postgresql

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestBatchColumnsUsesFieldOrder(t *testing.T) {
	batch := &plugin.BatchData{
		Fields: []plugin.FieldInfo{
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

	sql, args := buildPostgresInsertSQL("public", "target table", []string{"id", `city"name`}, rows)
	wantSQL := `INSERT INTO "public"."target table" ("id", "city""name") VALUES ($1, $2), ($3, $4)`
	if sql != wantSQL {
		t.Fatalf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []interface{}{1, "Hangzhou", 2, "Shanghai"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
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

func TestValidateBatchWriteModeRejectsOverwrite(t *testing.T) {
	err := validateBatchWriteMode("overwrite")
	if err == nil {
		t.Fatal("validateBatchWriteMode() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "table-level overwrite") {
		t.Fatalf("error = %q, want table-level overwrite guidance", err)
	}
}

func TestShouldUseCopyBatchWrite(t *testing.T) {
	if !shouldUseCopyBatchWrite(plugin.BatchWriteOptions{Method: "copy"}, nil) {
		t.Fatal("shouldUseCopyBatchWrite(copy) = false, want true")
	}
	if !shouldUseCopyBatchWrite(plugin.BatchWriteOptions{}, &plugin.BatchData{Metadata: map[string]interface{}{"write_method": "postgres_copy"}}) {
		t.Fatal("shouldUseCopyBatchWrite(metadata postgres_copy) = false, want true")
	}
	if shouldUseCopyBatchWrite(plugin.BatchWriteOptions{Method: "insert"}, nil) {
		t.Fatal("shouldUseCopyBatchWrite(insert) = true, want false")
	}
}

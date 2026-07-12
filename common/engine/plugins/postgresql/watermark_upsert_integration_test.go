package postgresql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationPostgresBoundedWatermarkResumeAndIdempotentUpsert(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx := context.Background()
	schema := "common_pg_it"
	sourceTable := fmt.Sprintf("watermark_source_%d", time.Now().UnixNano())
	targetTable := fmt.Sprintf("watermark_target_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schema, sourceTable, `"id" bigint PRIMARY KEY, "updated_at" timestamptz NOT NULL, "name" text`)
	defer dropPostgresPrepareTable(db, schema, sourceTable)
	defer dropPostgresPrepareTable(db, schema, targetTable)

	stamp := time.Date(2026, 7, 12, 8, 0, 0, 123456000, time.UTC)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO "%s"."%s" (id, updated_at, name) VALUES ($1,$2,$3),($4,$5,$6)`, schema, sourceTable), 1, stamp, "one", 2, stamp, "two"); err != nil {
		t.Fatalf("insert initial source rows: %v", err)
	}

	sourcePath := postgresPrepareTablePath(schema, sourceTable)
	session, err := pg.OpenBoundedWatermarkRead(ctx, connInfo, sourcePath, plugin.BoundedWatermarkReadOptions{WatermarkField: "updated_at", TieBreakers: []string{"id"}})
	if err != nil {
		t.Fatalf("OpenBoundedWatermarkRead failed: %v", err)
	}
	upper := session.UpperBound()
	if upper == nil || len(upper.Values) != 2 || upper.Values[1] != "2" {
		t.Fatalf("upper bound = %#v, want second row", upper)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO "%s"."%s" (id, updated_at, name) VALUES ($1,$2,$3),($4,$5,$6)`, schema, sourceTable), 3, stamp, "three", 4, stamp.Add(time.Second), "four"); err != nil {
		t.Fatalf("insert rows after upper bound: %v", err)
	}
	first, err := session.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("read first bounded batch: %v", err)
	}
	if len(first.Rows) != 2 || first.Rows[0]["id"] != int64(1) || first.Rows[1]["id"] != int64(2) {
		t.Fatalf("first bounded rows = %#v, want ids 1,2 only", first.Rows)
	}
	committed, err := session.PositionForRow(first.Rows[1])
	if err != nil {
		t.Fatalf("PositionForRow failed: %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("close first session: %v", err)
	}

	resume, err := pg.OpenBoundedWatermarkRead(ctx, connInfo, sourcePath, plugin.BoundedWatermarkReadOptions{WatermarkField: "updated_at", TieBreakers: []string{"id"}, Start: committed})
	if err != nil {
		t.Fatalf("open resumed watermark read: %v", err)
	}
	defer resume.Close(ctx)
	second, err := resume.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("read resumed batch: %v", err)
	}
	if len(second.Rows) != 2 || second.Rows[0]["id"] != int64(3) || second.Rows[1]["id"] != int64(4) {
		t.Fatalf("resumed rows = %#v, want ids 3,4", second.Rows)
	}
	secondCommitted, err := resume.PositionForRow(second.Rows[1])
	if err != nil {
		t.Fatalf("second PositionForRow failed: %v", err)
	}
	if err := resume.Close(ctx); err != nil {
		t.Fatalf("close resumed session: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`UPDATE "%s"."%s" SET name = 'changed-without-watermark' WHERE id = 1`, schema, sourceTable)); err != nil {
		t.Fatalf("update row without watermark: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO "%s"."%s" (id, updated_at, name) VALUES ($1,$2,$3)`, schema, sourceTable), 5, stamp.Add(-time.Second), "backdated"); err != nil {
		t.Fatalf("insert backdated row: %v", err)
	}
	third, err := pg.OpenBoundedWatermarkRead(ctx, connInfo, sourcePath, plugin.BoundedWatermarkReadOptions{WatermarkField: "updated_at", TieBreakers: []string{"id"}, Start: secondCommitted})
	if err != nil {
		t.Fatalf("open third watermark read: %v", err)
	}
	defer third.Close(ctx)
	unchanged, err := third.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("read third batch: %v", err)
	}
	if len(unchanged.Rows) != 0 {
		t.Fatalf("rows with unchanged/backdated watermark were returned: %#v", unchanged.Rows)
	}

	targetPath := postgresPrepareTablePath(schema, targetTable)
	upsertOpts := plugin.TableUpsertOptions{Fields: []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "updated_at", Type: datatype.FieldTypeTimestamp},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
	}, Keys: []string{"id"}}
	if err := pg.PrepareTableUpsert(ctx, connInfo, targetPath, upsertOpts); err != nil {
		t.Fatalf("PrepareTableUpsert failed: %v", err)
	}
	batch := &plugin.BatchData{Fields: upsertOpts.Fields, Rows: []map[string]interface{}{{"id": int64(1), "updated_at": stamp, "name": "first"}}}
	if err := pg.UpsertBatch(ctx, connInfo, targetPath, batch, upsertOpts); err != nil {
		t.Fatalf("first UpsertBatch failed: %v", err)
	}
	batch.Rows[0]["name"] = "updated"
	if err := pg.UpsertBatch(ctx, connInfo, targetPath, batch, upsertOpts); err != nil {
		t.Fatalf("repeated UpsertBatch failed: %v", err)
	}
	var count int
	var name string
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*), max(name) FROM "%s"."%s" WHERE id = 1`, schema, targetTable)).Scan(&count, &name); err != nil {
		t.Fatalf("query upsert target: %v", err)
	}
	if count != 1 || name != "updated" {
		t.Fatalf("upsert target count=%d name=%q, want one updated row", count, name)
	}
}

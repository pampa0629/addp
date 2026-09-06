package oceanbase

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationOceanBaseBoundedWatermarkResumeAndIdempotentUpsert(t *testing.T) {
	if os.Getenv("ADDP_OCEANBASE_INTEGRATION") != "1" {
		t.Skip("set ADDP_OCEANBASE_INTEGRATION=1 to run OceanBase integration test")
	}

	const engineID uint = 94004
	const sourceTable = "addp_watermark_source_gate"
	const targetTable = "addp_watermark_target_gate"
	p := &Plugin{}
	connInfo := oceanBaseIntegrationConnInfo()
	database := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable")
	sourcePath := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, sourceTable)
	targetPath := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, targetTable)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN() error = %v", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, path := range []plugin.EngineCatalogPath{sourcePath, targetPath} {
		if err := p.DeleteResource(ctx, connInfo, path); err != nil {
			t.Fatalf("drop stale watermark gate table error = %v", err)
		}
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		for _, path := range []plugin.EngineCatalogPath{sourcePath, targetPath} {
			if err := p.DeleteResource(cleanupContext, connInfo, path); err != nil {
				t.Errorf("cleanup watermark gate table error = %v", err)
			}
		}
	})

	qualifiedSource := fmt.Sprintf("`%s`.`%s`", database, sourceTable)
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualifiedSource+" (`id` BIGINT NOT NULL PRIMARY KEY, `updated_at` DATETIME(6) NOT NULL, `name` VARCHAR(255) NOT NULL) ENGINE=InnoDB"); err != nil {
		t.Fatalf("create OceanBase watermark source: %v", err)
	}
	stamp := time.Date(2026, time.September, 6, 8, 0, 0, 123456000, time.UTC)
	if _, err := db.ExecContext(ctx, "INSERT INTO "+qualifiedSource+" (id, updated_at, name) VALUES (?, ?, ?), (?, ?, ?)", 1, stamp, "one", 2, stamp, "two"); err != nil {
		t.Fatalf("insert initial OceanBase watermark rows: %v", err)
	}

	session, err := p.OpenBoundedWatermarkRead(ctx, connInfo, sourcePath, plugin.BoundedWatermarkReadOptions{WatermarkField: "updated_at", TieBreakers: []string{"id"}})
	if err != nil {
		t.Fatalf("OpenBoundedWatermarkRead() error = %v", err)
	}
	upper := session.UpperBound()
	if upper == nil || len(upper.Values) != 2 || upper.Values[1] != "2" {
		t.Fatalf("upper bound = %#v, want id 2", upper)
	}
	if _, err := db.ExecContext(ctx, "UPDATE "+qualifiedSource+" SET updated_at = ?, name = ? WHERE id = ?", stamp.Add(time.Second), "changed-after-snapshot", 2); err != nil {
		t.Fatalf("update source after snapshot: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+qualifiedSource+" (id, updated_at, name) VALUES (?, ?, ?)", 3, stamp, "three"); err != nil {
		t.Fatalf("insert same-watermark row after snapshot: %v", err)
	}

	first, err := session.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("read first OceanBase watermark batch: %v", err)
	}
	if len(first.Rows) != 2 || fmt.Sprint(first.Rows[1]["name"]) != "two" {
		t.Fatalf("first snapshot rows = %#v, want two original rows", first.Rows)
	}
	committed, err := session.PositionForRow(first.Rows[len(first.Rows)-1])
	if err != nil {
		t.Fatalf("PositionForRow() error = %v", err)
	}
	tableInfo, spatialInfo := session.TableInfo()
	if spatialInfo != nil {
		t.Fatalf("OceanBase watermark table unexpectedly declared spatial info: %#v", spatialInfo)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("close first OceanBase watermark session: %v", err)
	}

	upsertOptions := plugin.TableUpsertOptions{Fields: tableInfo.Fields, Keys: []string{"id"}}
	if err := p.PrepareTableUpsert(ctx, connInfo, targetPath, upsertOptions); err != nil {
		t.Fatalf("PrepareTableUpsert() error = %v", err)
	}
	if err := p.UpsertBatch(ctx, connInfo, targetPath, first, upsertOptions); err != nil {
		t.Fatalf("upsert first watermark batch: %v", err)
	}

	resume, err := p.OpenBoundedWatermarkRead(ctx, connInfo, sourcePath, plugin.BoundedWatermarkReadOptions{WatermarkField: "updated_at", TieBreakers: []string{"id"}, Start: committed})
	if err != nil {
		t.Fatalf("open resumed OceanBase watermark read: %v", err)
	}
	defer resume.Close(ctx)
	second, err := resume.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("read resumed OceanBase watermark batch: %v", err)
	}
	if len(second.Rows) != 2 || fmt.Sprint(second.Rows[0]["id"]) != "3" || fmt.Sprint(second.Rows[1]["id"]) != "2" {
		t.Fatalf("resumed rows = %#v, want same-watermark id 3 followed by updated id 2", second.Rows)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.UpsertBatch(ctx, connInfo, targetPath, second, upsertOptions); err != nil {
			t.Fatalf("idempotent resumed upsert attempt %d error = %v", attempt+1, err)
		}
	}

	qualifiedTarget := fmt.Sprintf("`%s`.`%s`", database, targetTable)
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualifiedTarget).Scan(&count); err != nil {
		t.Fatalf("count OceanBase watermark target: %v", err)
	}
	if count != 3 {
		t.Fatalf("target row count = %d, want 3", count)
	}
	var updatedName string
	if err := db.QueryRowContext(ctx, "SELECT name FROM "+qualifiedTarget+" WHERE id = 2").Scan(&updatedName); err != nil {
		t.Fatalf("read updated OceanBase watermark target row: %v", err)
	}
	if updatedName != "changed-after-snapshot" {
		t.Fatalf("target row 2 name = %q, want changed-after-snapshot", updatedName)
	}
}

package mysql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationMySQLBoundedWatermarkResumeAndIdempotentUpsert(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := context.Background()
	sourceTable := "watermark_source"
	targetTable := "watermark_target"
	sourceQualified := mysqlDialect().QualifiedTable(database, sourceTable)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id BIGINT NOT NULL PRIMARY KEY,
			updated_at DATETIME(6) NOT NULL,
			name VARCHAR(255) NOT NULL
		) ENGINE=InnoDB`, sourceQualified)); err != nil {
		t.Fatalf("create MySQL watermark source: %v", err)
	}
	stamp := time.Date(2026, 7, 12, 8, 0, 0, 123456000, time.UTC)
	if _, err := db.ExecContext(ctx,
		"INSERT INTO "+sourceQualified+" (id, updated_at, name) VALUES (?, ?, ?), (?, ?, ?)",
		1, stamp, "one", 2, stamp, "two",
	); err != nil {
		t.Fatalf("insert initial MySQL watermark rows: %v", err)
	}

	sourcePath := mysqlIntegrationTablePath(database, sourceTable)
	session, err := mysqlPlugin.OpenBoundedWatermarkRead(ctx, connInfo, sourcePath, plugin.BoundedWatermarkReadOptions{
		WatermarkField: "updated_at", TieBreakers: []string{"id"},
	})
	if err != nil {
		t.Fatalf("OpenBoundedWatermarkRead failed: %v", err)
	}
	upper := session.UpperBound()
	if upper == nil || len(upper.Values) != 2 || upper.Values[1] != "2" {
		t.Fatalf("upper bound = %#v, want id 2", upper)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE "+sourceQualified+" SET updated_at = ?, name = ? WHERE id = ?",
		stamp.Add(time.Second), "changed-after-snapshot", 2,
	); err != nil {
		t.Fatalf("update source after snapshot: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO "+sourceQualified+" (id, updated_at, name) VALUES (?, ?, ?)",
		3, stamp, "three",
	); err != nil {
		t.Fatalf("insert same-watermark row after snapshot: %v", err)
	}

	first, err := session.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("read first MySQL watermark batch: %v", err)
	}
	if len(first.Rows) != 2 || fmt.Sprint(first.Rows[1]["name"]) != "two" {
		t.Fatalf("first snapshot rows = %#v, want two original rows", first.Rows)
	}
	committed, err := session.PositionForRow(first.Rows[len(first.Rows)-1])
	if err != nil {
		t.Fatalf("PositionForRow failed: %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("close first MySQL watermark session: %v", err)
	}

	targetPath := mysqlIntegrationTablePath(database, targetTable)
	tableInfo, spatialInfo := session.TableInfo()
	upsertOptions := plugin.TableUpsertOptions{Fields: tableInfo.Fields, SpatialInfo: spatialInfo, Keys: []string{"id"}}
	if err := mysqlPlugin.PrepareTableUpsert(ctx, connInfo, targetPath, upsertOptions); err != nil {
		t.Fatalf("prepare MySQL watermark target: %v", err)
	}
	if err := mysqlPlugin.UpsertBatch(ctx, connInfo, targetPath, first, upsertOptions); err != nil {
		t.Fatalf("upsert first MySQL watermark batch: %v", err)
	}

	resume, err := mysqlPlugin.OpenBoundedWatermarkRead(ctx, connInfo, sourcePath, plugin.BoundedWatermarkReadOptions{
		WatermarkField: "updated_at", TieBreakers: []string{"id"}, Start: committed,
	})
	if err != nil {
		t.Fatalf("open resumed MySQL watermark read: %v", err)
	}
	defer resume.Close(ctx)
	second, err := resume.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("read resumed MySQL watermark batch: %v", err)
	}
	if len(second.Rows) != 2 || fmt.Sprint(second.Rows[0]["id"]) != "3" || fmt.Sprint(second.Rows[1]["id"]) != "2" {
		t.Fatalf("resumed rows = %#v, want same-watermark id 3 followed by updated id 2", second.Rows)
	}
	if err := mysqlPlugin.UpsertBatch(ctx, connInfo, targetPath, second, upsertOptions); err != nil {
		t.Fatalf("upsert resumed MySQL watermark batch: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+mysqlDialect().QualifiedTable(database, targetTable)).Scan(&count); err != nil {
		t.Fatalf("count MySQL watermark target: %v", err)
	}
	if count != 3 {
		t.Fatalf("target row count = %d, want 3", count)
	}
	var updatedName string
	if err := db.QueryRowContext(ctx,
		"SELECT name FROM "+mysqlDialect().QualifiedTable(database, targetTable)+" WHERE id = 2",
	).Scan(&updatedName); err != nil {
		t.Fatalf("read updated MySQL watermark target row: %v", err)
	}
	if updatedName != "changed-after-snapshot" {
		t.Fatalf("target row 2 name = %q, want changed-after-snapshot", updatedName)
	}
}

func TestIntegrationMySQLBoundedWatermarkRejectsNonInnoDBSource(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := context.Background()
	qualified := mysqlDialect().QualifiedTable(database, "watermark_myisam")
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+" (id BIGINT NOT NULL PRIMARY KEY, updated_at DATETIME(6) NOT NULL) ENGINE=MyISAM"); err != nil {
		t.Fatalf("create MyISAM watermark source: %v", err)
	}
	if _, err := mysqlPlugin.OpenBoundedWatermarkRead(ctx, connInfo, mysqlIntegrationTablePath(database, "watermark_myisam"), plugin.BoundedWatermarkReadOptions{
		WatermarkField: "updated_at", TieBreakers: []string{"id"},
	}); err == nil {
		t.Fatal("OpenBoundedWatermarkRead accepted a non-InnoDB source")
	}
}

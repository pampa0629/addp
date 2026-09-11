package tidb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationTiDBCatalogQueryAndWrite(t *testing.T) {
	if os.Getenv("ADDP_TIDB_INTEGRATION") != "1" {
		t.Skip("set ADDP_TIDB_INTEGRATION=1 to run TiDB integration test")
	}

	const engineID uint = 95001
	const sourceTable = "addp_catalog_source_gate"
	const targetTable = "addp_table_write_gate"
	p := &Plugin{}
	connInfo := tidbIntegrationConnInfo()
	databaseName := tidbIntegrationEnv("ADDP_TEST_TIDB_DATABASE", "addp_tidb_disposable")
	sourcePath := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, databaseName, sourceTable)
	targetPath := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, databaseName, targetTable)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })

	if err := p.TestConnection(ctx, connInfo); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
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
			t.Fatalf("drop stale gate table error = %v", err)
		}
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		for _, path := range []plugin.EngineCatalogPath{sourcePath, targetPath} {
			if err := p.DeleteResource(cleanupContext, connInfo, path); err != nil {
				t.Errorf("cleanup gate table error = %v", err)
			}
		}
	})

	if _, err := db.ExecContext(ctx, "CREATE TABLE `"+sourceTable+"` (`id` BIGINT NOT NULL PRIMARY KEY, `name` VARCHAR(255) NOT NULL, `amount` DECIMAL(18,2) NOT NULL, `active` TINYINT(1) NOT NULL, `created_at` DATETIME(6) NOT NULL) ENGINE=InnoDB"); err != nil {
		t.Fatalf("create source fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+sourceTable+"` VALUES (?, ?, ?, ?, ?)", 1, "TiDB 8.5.8", "88.50", true, time.Date(2026, time.September, 11, 8, 0, 0, 123456000, time.UTC)); err != nil {
		t.Fatalf("insert source fixture: %v", err)
	}

	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), engineID)
	databases, err := p.ListChildren(ctx, connInfo, root, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	for _, systemDatabase := range []string{"information_schema", "inspection_schema", "metrics_schema", "mysql", "performance_schema", "sys"} {
		if findTiDBCatalogEntry(databases, systemDatabase) != nil {
			t.Fatalf("system database %q must be filtered from %#v", systemDatabase, tidbCatalogEntryNames(databases))
		}
	}
	databaseEntry := findTiDBCatalogEntry(databases, databaseName)
	if databaseEntry == nil {
		t.Fatalf("database %q not found in %#v", databaseName, tidbCatalogEntryNames(databases))
	}
	items, err := p.ListChildren(ctx, connInfo, databaseEntry.Path, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(database) error = %v", err)
	}
	sourceEntry := findTiDBCatalogEntry(items, sourceTable)
	if sourceEntry == nil || sourceEntry.Kind != plugin.EngineCatalogKindTable {
		t.Fatalf("source table not found in %#v", tidbCatalogEntryNames(items))
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, sourceEntry.Path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true, IncludeConstraints: true})
	if err != nil {
		t.Fatalf("DescribeEngineCatalogFacts() error = %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 1 {
		t.Fatalf("source facts = %#v, want row_count=1", facts.Table)
	}
	assertTiDBIntegrationField(t, facts.Table.Fields, "id", datatype.FieldTypeBigInt, true)
	assertTiDBIntegrationField(t, facts.Table.Fields, "amount", datatype.FieldTypeDecimal, false)
	assertTiDBIntegrationField(t, facts.Table.Fields, "active", datatype.FieldTypeBool, false)

	prepared, err := p.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID:   engineID,
		Language:   "sql",
		Query:      "SELECT name, amount FROM `" + databaseName + "`.`" + sourceTable + "` WHERE id = :id",
		TargetPath: &sourceEntry.Path,
		Options:    plugin.QueryOptions{ReadOnly: true, Limit: 1, Parameters: map[string]interface{}{"id": 1}},
	})
	if err != nil {
		t.Fatalf("PrepareQuery() error = %v", err)
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil || len(readSet.Paths) != 1 || readSet.Paths[0].StringPath() != databaseName+"/"+sourceTable {
		t.Fatalf("PreparedQuery.ReadSet() = %#v, %v", readSet.Paths, err)
	}
	result, err := prepared.Execute(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.Execute() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["name"] != "TiDB 8.5.8" {
		t.Fatalf("PreparedQuery.Execute() rows = %#v", result.Rows)
	}

	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true, Nullable: false},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: false},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2, Nullable: false},
		{Name: "updated_at", Type: datatype.FieldTypeTimestamp, Nullable: false},
	}
	if err := p.PrepareTableWrite(ctx, connInfo, targetPath, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatalf("PrepareTableWrite() error = %v", err)
	}
	session, err := p.OpenTableWriteSession(ctx, connInfo, targetPath, plugin.TableWriteSessionOptions{Method: "copy", Fields: fields})
	if err != nil {
		t.Fatalf("OpenTableWriteSession() error = %v", err)
	}
	stamp := time.Date(2026, time.September, 11, 9, 0, 0, 654321000, time.UTC)
	batch := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"id": int64(1), "name": "首次写入", "amount": "12.34", "updated_at": stamp}}}
	if err := session.WriteBatch(ctx, batch); err != nil {
		_ = session.Abort(ctx)
		t.Fatalf("WriteBatch() error = %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	markerProvider, ok := session.(plugin.CommitMarkerProvider)
	if !ok || markerProvider.CommitMarker() == nil || markerProvider.CommitMarker().Provider != "tidb.table_write_session" {
		t.Fatalf("TiDB write session commit marker = %#v", markerProvider)
	}
	upsertOptions := plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}}
	if err := p.PrepareTableUpsert(ctx, connInfo, targetPath, upsertOptions); err != nil {
		t.Fatalf("PrepareTableUpsert() error = %v", err)
	}
	changed := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"id": int64(1), "name": "幂等更新", "amount": "56.78", "updated_at": stamp.Add(time.Microsecond)}}}
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.UpsertBatch(ctx, connInfo, targetPath, changed, upsertOptions); err != nil {
			t.Fatalf("UpsertBatch() attempt %d error = %v", attempt+1, err)
		}
	}
	var name string
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*), MAX(name) FROM `"+targetTable+"`").Scan(&count, &name); err != nil {
		t.Fatalf("query target rows: %v", err)
	}
	if count != 1 || name != "幂等更新" {
		t.Fatalf("target state = count %d, name %q", count, name)
	}
}

func TestIntegrationTiDBBoundedWatermarkResumeAndIdempotentUpsert(t *testing.T) {
	if os.Getenv("ADDP_TIDB_INTEGRATION") != "1" {
		t.Skip("set ADDP_TIDB_INTEGRATION=1 to run TiDB integration test")
	}

	const engineID uint = 95002
	const sourceTable = "addp_watermark_source_gate"
	const targetTable = "addp_watermark_target_gate"
	p := &Plugin{}
	connInfo := tidbIntegrationConnInfo()
	database := tidbIntegrationEnv("ADDP_TEST_TIDB_DATABASE", "addp_tidb_disposable")
	sourcePath := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, sourceTable)
	targetPath := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, targetTable)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
			t.Fatalf("drop stale watermark gate table: %v", err)
		}
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		for _, path := range []plugin.EngineCatalogPath{sourcePath, targetPath} {
			if err := p.DeleteResource(cleanupContext, connInfo, path); err != nil {
				t.Errorf("cleanup watermark gate table: %v", err)
			}
		}
	})

	qualifiedSource := fmt.Sprintf("`%s`.`%s`", database, sourceTable)
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualifiedSource+" (`id` BIGINT NOT NULL PRIMARY KEY, `updated_at` DATETIME(6) NOT NULL, `name` VARCHAR(255) NOT NULL) ENGINE=InnoDB"); err != nil {
		t.Fatalf("create watermark source: %v", err)
	}
	stamp := time.Date(2026, time.September, 11, 10, 0, 0, 123456000, time.UTC)
	if _, err := db.ExecContext(ctx, "INSERT INTO "+qualifiedSource+" VALUES (?, ?, ?), (?, ?, ?)", 1, stamp, "one", 2, stamp, "two"); err != nil {
		t.Fatalf("insert initial watermark rows: %v", err)
	}
	session, err := p.OpenBoundedWatermarkRead(ctx, connInfo, sourcePath, plugin.BoundedWatermarkReadOptions{WatermarkField: "updated_at", TieBreakers: []string{"id"}})
	if err != nil {
		t.Fatalf("OpenBoundedWatermarkRead() error = %v", err)
	}
	if upper := session.UpperBound(); upper == nil || len(upper.Values) != 2 || upper.Values[1] != "2" {
		t.Fatalf("upper bound = %#v, want id 2", upper)
	}
	if _, err := db.ExecContext(ctx, "UPDATE "+qualifiedSource+" SET updated_at = ?, name = ? WHERE id = ?", stamp.Add(time.Second), "changed-after-snapshot", 2); err != nil {
		t.Fatalf("update source after snapshot: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+qualifiedSource+" VALUES (?, ?, ?)", 3, stamp, "three"); err != nil {
		t.Fatalf("insert same-watermark row after snapshot: %v", err)
	}
	first, err := session.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("read first watermark batch: %v", err)
	}
	if len(first.Rows) != 2 || fmt.Sprint(first.Rows[1]["name"]) != "two" {
		t.Fatalf("first snapshot rows = %#v", first.Rows)
	}
	committed, err := session.PositionForRow(first.Rows[len(first.Rows)-1])
	if err != nil {
		t.Fatalf("PositionForRow() error = %v", err)
	}
	tableInfo, spatialInfo := session.TableInfo()
	if spatialInfo != nil {
		t.Fatalf("TiDB watermark table unexpectedly declared spatial info: %#v", spatialInfo)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("close first watermark session: %v", err)
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
		t.Fatalf("open resumed watermark read: %v", err)
	}
	defer resume.Close(ctx)
	second, err := resume.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("read resumed watermark batch: %v", err)
	}
	if len(second.Rows) != 2 || fmt.Sprint(second.Rows[0]["id"]) != "3" || fmt.Sprint(second.Rows[1]["id"]) != "2" {
		t.Fatalf("resumed rows = %#v", second.Rows)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.UpsertBatch(ctx, connInfo, targetPath, second, upsertOptions); err != nil {
			t.Fatalf("idempotent resumed upsert attempt %d: %v", attempt+1, err)
		}
	}
	qualifiedTarget := fmt.Sprintf("`%s`.`%s`", database, targetTable)
	var count int
	var updatedName string
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualifiedTarget).Scan(&count); err != nil {
		t.Fatalf("count watermark target: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT name FROM "+qualifiedTarget+" WHERE id = 2").Scan(&updatedName); err != nil {
		t.Fatalf("read updated target row: %v", err)
	}
	if count != 3 || updatedName != "changed-after-snapshot" {
		t.Fatalf("target state = count %d, name %q", count, updatedName)
	}
}

func tidbIntegrationConnInfo() plugin.ConnectionInfo {
	return plugin.ConnectionInfo{
		"host":     tidbIntegrationEnv("ADDP_TEST_TIDB_HOST", "127.0.0.1"),
		"port":     tidbIntegrationEnv("ADDP_TEST_TIDB_PORT", "4000"),
		"database": tidbIntegrationEnv("ADDP_TEST_TIDB_DATABASE", "addp_tidb_disposable"),
		"user":     tidbIntegrationEnv("ADDP_TEST_TIDB_USER", "root"),
		"password": tidbIntegrationEnv("ADDP_TEST_TIDB_PASSWORD", ""),
	}
}

func tidbIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func findTiDBCatalogEntry(entries []plugin.EngineCatalogEntry, name string) *plugin.EngineCatalogEntry {
	for index := range entries {
		if strings.EqualFold(entries[index].Name, name) {
			return &entries[index]
		}
	}
	return nil
}

func tidbCatalogEntryNames(entries []plugin.EngineCatalogEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names
}

func assertTiDBIntegrationField(t *testing.T, fields []datatype.FieldInfo, name string, fieldType datatype.FieldType, primaryKey bool) {
	t.Helper()
	for _, field := range fields {
		if field.Name == name {
			if field.Type != fieldType || field.PrimaryKey != primaryKey {
				t.Fatalf("field %q = %#v, want type=%q primary_key=%v", name, field, fieldType, primaryKey)
			}
			return
		}
	}
	t.Fatalf("field %q not found in %#v", name, fields)
}

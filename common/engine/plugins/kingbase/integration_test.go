package kingbase

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationKingbaseProviderContract(t *testing.T) {
	if os.Getenv("ADDP_KINGBASE_INTEGRATION") != "1" {
		t.Skip("set ADDP_KINGBASE_INTEGRATION=1 to run KingbaseES integration test")
	}

	const engineID uint = 97001
	const schemaName = "addp_kingbase_gate"
	const tableName = "provider_contract"
	p := &Plugin{}
	connInfo := kingbaseIntegrationConnInfo()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })

	if err := p.TestConnection(ctx, connInfo); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN() error = %v", err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`); err != nil {
		t.Fatalf("drop stale gate schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupContext, `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`); err != nil {
			t.Errorf("cleanup gate schema: %v", err)
		}
	})

	path := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermSchema, schemaName, tableName)
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true, Nullable: false},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: false},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2, Nullable: false},
		{Name: "active", Type: datatype.FieldTypeBool, Nullable: false},
		{Name: "updated_at", Type: datatype.FieldTypeTimestamp, Nullable: false},
	}
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatalf("PrepareTableWrite() error = %v", err)
	}
	session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Method: "copy", Fields: fields})
	if err != nil {
		t.Fatalf("OpenTableWriteSession() error = %v", err)
	}
	baseTime := time.Date(2026, time.September, 12, 9, 0, 0, 123000000, time.UTC)
	if err := session.WriteBatch(ctx, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{
		{"id": int64(1), "name": "长沙", "amount": "10.25", "active": true, "updated_at": baseTime},
		{"id": int64(2), "name": "武汉", "amount": "20.50", "active": false, "updated_at": baseTime.Add(time.Microsecond)},
	}}); err != nil {
		_ = session.Abort(ctx)
		t.Fatalf("WriteBatch() error = %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("table write Close() error = %v", err)
	}
	markerProvider, ok := session.(plugin.CommitMarkerProvider)
	if !ok {
		t.Fatal("KingbaseES table write session must expose a commit marker")
	}
	marker := markerProvider.CommitMarker()
	if marker == nil || marker.Provider != "kingbase.table_write_session" {
		t.Fatalf("KingbaseES write marker = %#v", marker)
	}

	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), engineID)
	schemas, err := p.ListChildren(ctx, connInfo, root, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	for _, systemSchema := range kingbaseSystemSchemas {
		if findCatalogEntry(schemas, systemSchema) != nil {
			t.Fatalf("KingbaseES system schema %q leaked into catalog", systemSchema)
		}
	}
	publicSchema := findCatalogEntry(schemas, "public")
	if publicSchema == nil {
		t.Fatal("public schema not found")
	}
	publicTables, err := p.ListChildren(ctx, connInfo, publicSchema.Path, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(public) error = %v", err)
	}
	for _, systemTable := range kingbaseSystemTables {
		if findCatalogEntry(publicTables, systemTable) != nil {
			t.Fatalf("KingbaseES system table %q leaked into public catalog", systemTable)
		}
		hiddenPath := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermSchema, "public", systemTable)
		if _, err := p.ResolvePath(ctx, connInfo, hiddenPath); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
			t.Fatalf("ResolvePath(%q) error = %v, want not_found", systemTable, err)
		}
	}
	schema := findCatalogEntry(schemas, schemaName)
	if schema == nil {
		t.Fatalf("schema %q not found", schemaName)
	}
	tables, err := p.ListChildren(ctx, connInfo, schema.Path, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(schema) error = %v", err)
	}
	table := findCatalogEntry(tables, tableName)
	if table == nil {
		t.Fatalf("table %q not found", tableName)
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, table.Path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true, IncludeConstraints: true})
	if err != nil {
		t.Fatalf("DescribeEngineCatalogFacts() error = %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 2 {
		t.Fatalf("table facts = %#v, want row_count=2", facts.Table)
	}

	prepared, err := p.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: engineID,
		Language: "sql",
		Query:    `SELECT name, amount FROM ` + schemaName + `.` + tableName + ` WHERE id = :id`,
		Options: plugin.QueryOptions{
			ReadOnly:   true,
			Limit:      1,
			Parameters: map[string]interface{}{"id": 1},
		},
	})
	if err != nil {
		t.Fatalf("PrepareQuery() error = %v", err)
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil || len(readSet.Paths) != 1 || readSet.Paths[0].StringPath() != schemaName+"/"+tableName {
		t.Fatalf("PreparedQuery.ReadSet() = %#v, error = %v", readSet.Paths, err)
	}
	result, err := prepared.Execute(ctx)
	if err != nil || len(result.Rows) != 1 || result.Rows[0]["name"] != "长沙" {
		t.Fatalf("PreparedQuery.Execute() rows = %#v, error = %v", result.Rows, err)
	}

	watermark, err := p.OpenBoundedWatermarkRead(ctx, connInfo, table.Path, plugin.BoundedWatermarkReadOptions{
		WatermarkField: "updated_at",
		TieBreakers:    []string{"id"},
	})
	if err != nil {
		t.Fatalf("OpenBoundedWatermarkRead() error = %v", err)
	}
	watermarkBatch, err := watermark.ReadBatch(ctx, 10)
	if err != nil || len(watermarkBatch.Rows) != 2 || watermark.UpperBound() == nil {
		t.Fatalf("watermark result rows=%#v upper=%#v error=%v", watermarkBatch.Rows, watermark.UpperBound(), err)
	}
	if err := watermark.Close(ctx); err != nil {
		t.Fatalf("watermark Close() error = %v", err)
	}

	upsertOptions := plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}}
	if err := p.PrepareTableUpsert(ctx, connInfo, table.Path, upsertOptions); err != nil {
		t.Fatalf("PrepareTableUpsert() error = %v", err)
	}
	changed := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{
		{"id": int64(2), "name": "武汉更新", "amount": "21.00", "active": true, "updated_at": baseTime.Add(2 * time.Microsecond)},
		{"id": int64(3), "name": "西安", "amount": "30.75", "active": true, "updated_at": baseTime.Add(3 * time.Microsecond)},
	}}
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.UpsertBatch(ctx, connInfo, table.Path, changed, upsertOptions); err != nil {
			t.Fatalf("UpsertBatch() attempt %d error = %v", attempt+1, err)
		}
	}
	var rowCount int
	var updatedName string
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(CASE WHEN id=2 THEN name ELSE '' END) FROM `+schemaName+`.`+tableName).Scan(&rowCount, &updatedName); err != nil {
		t.Fatalf("query upsert result: %v", err)
	}
	if rowCount != 3 || updatedName != "武汉更新" {
		t.Fatalf("upsert result row_count=%d updated_name=%q", rowCount, updatedName)
	}

	if err := p.DeleteResource(ctx, connInfo, table.Path); err != nil {
		t.Fatalf("DeleteResource() error = %v", err)
	}
	if _, err := p.ResolvePath(ctx, connInfo, table.Path); err == nil {
		t.Fatal("ResolvePath() after delete error = nil")
	}
}

func kingbaseIntegrationConnInfo() plugin.ConnectionInfo {
	port, _ := strconv.Atoi(envOrDefault("ADDP_TEST_KINGBASE_PORT", "54321"))
	return plugin.ConnectionInfo{
		"host":     envOrDefault("ADDP_TEST_KINGBASE_HOST", "127.0.0.1"),
		"port":     port,
		"database": envOrDefault("ADDP_TEST_KINGBASE_DATABASE", "business"),
		"user":     envOrDefault("ADDP_TEST_KINGBASE_USER", "system"),
		"password": os.Getenv("ADDP_TEST_KINGBASE_PASSWORD"),
		"sslmode":  "disable",
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func findCatalogEntry(entries []plugin.EngineCatalogEntry, name string) *plugin.EngineCatalogEntry {
	for index := range entries {
		if entries[index].Name == name {
			return &entries[index]
		}
	}
	return nil
}

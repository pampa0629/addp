package opengauss

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationOpenGaussProviderContract(t *testing.T) {
	if os.Getenv("ADDP_OPENGAUSS_INTEGRATION") != "1" {
		t.Skip("set ADDP_OPENGAUSS_INTEGRATION=1 to run openGauss integration test")
	}

	const engineID uint = 96001
	const schemaName = "addp_opengauss_gate"
	const tableName = "provider_contract"
	p := &Plugin{}
	connInfo := openGaussIntegrationConnInfo()
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
	baseTime := time.Date(2026, time.September, 7, 8, 0, 0, 123000000, time.UTC)
	initial := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{
		{"id": int64(1), "name": "长沙", "amount": "10.25", "active": true, "updated_at": baseTime},
		{"id": int64(2), "name": "武汉", "amount": "20.50", "active": false, "updated_at": baseTime.Add(time.Microsecond)},
	}}
	if err := session.WriteBatch(ctx, initial); err != nil {
		_ = session.Abort(ctx)
		t.Fatalf("WriteBatch() error = %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("table write Close() error = %v", err)
	}
	markerProvider, ok := session.(plugin.CommitMarkerProvider)
	if !ok {
		t.Fatal("openGauss table write session must expose a commit marker")
	}
	marker := markerProvider.CommitMarker()
	if marker == nil || marker.Provider != "opengauss.table_write_session" || marker.CommitPosition["rows_committed"] != int64(2) {
		t.Fatalf("CommitMarker() = %#v", marker)
	}

	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), engineID)
	schemas, err := p.ListChildren(ctx, connInfo, root, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	schema := findOpenGaussCatalogEntry(schemas, schemaName)
	if schema == nil || schema.Term != plugin.EngineCatalogTermSchema {
		t.Fatalf("schema %q not found in %#v", schemaName, catalogEntryNames(schemas))
	}
	tables, err := p.ListChildren(ctx, connInfo, schema.Path, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(schema) error = %v", err)
	}
	table := findOpenGaussCatalogEntry(tables, tableName)
	if table == nil || table.Path.StringPath() != schemaName+"/"+tableName {
		t.Fatalf("table %q not found in %#v", tableName, catalogEntryNames(tables))
	}

	facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, table.Path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true, IncludeConstraints: true})
	if err != nil {
		t.Fatalf("DescribeEngineCatalogFacts() error = %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 2 {
		t.Fatalf("table facts = %#v, want row_count=2", facts.Table)
	}
	assertOpenGaussField(t, facts.Table.Fields, "id", datatype.FieldTypeBigInt, true)
	assertOpenGaussField(t, facts.Table.Fields, "amount", datatype.FieldTypeDecimal, false)
	assertOpenGaussField(t, facts.Table.Fields, "active", datatype.FieldTypeBool, false)

	batch, err := p.ReadBatch(ctx, connInfo, table.Path, plugin.BatchReadOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ReadBatch() error = %v", err)
	}
	if len(batch.Rows) != 2 {
		t.Fatalf("ReadBatch() rows = %#v", batch.Rows)
	}

	prepared, err := p.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID:   engineID,
		Language:   "sql",
		Query:      `SELECT name, amount FROM ` + schemaName + `.` + tableName + ` WHERE id = :id`,
		TargetPath: &table.Path,
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
	if err != nil {
		t.Fatalf("PreparedQuery.ReadSet() error = %v", err)
	}
	if len(readSet.Paths) != 1 || readSet.Paths[0].StringPath() != schemaName+"/"+tableName {
		t.Fatalf("PreparedQuery.ReadSet() = %#v", readSet.Paths)
	}
	result, err := prepared.Execute(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.Execute() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["name"] != "长沙" {
		t.Fatalf("PreparedQuery.Execute() rows = %#v", result.Rows)
	}

	streamPlan, err := p.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID:   engineID,
		Language:   "sql",
		Query:      `SELECT id, name FROM ` + schemaName + `.` + tableName + ` ORDER BY id`,
		TargetPath: &table.Path,
		Options:    plugin.QueryOptions{ReadOnly: true},
	})
	if err != nil {
		t.Fatalf("PrepareQuery(stream) error = %v", err)
	}
	querySession, err := p.OpenQueryReadSession(ctx, streamPlan)
	if err != nil {
		t.Fatalf("OpenQueryReadSession() error = %v", err)
	}
	queryBatch, err := querySession.ReadBatch(ctx, 1)
	if err != nil {
		t.Fatalf("query ReadBatch() error = %v", err)
	}
	if len(queryBatch.Rows) != 1 || queryBatch.Rows[0]["name"] != "长沙" {
		t.Fatalf("query ReadBatch() rows = %#v", queryBatch.Rows)
	}
	if err := querySession.Close(ctx); err != nil {
		t.Fatalf("query session Close() error = %v", err)
	}

	watermark, err := p.OpenBoundedWatermarkRead(ctx, connInfo, table.Path, plugin.BoundedWatermarkReadOptions{
		WatermarkField: "updated_at",
		TieBreakers:    []string{"id"},
	})
	if err != nil {
		t.Fatalf("OpenBoundedWatermarkRead() error = %v", err)
	}
	watermarkBatch, err := watermark.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("watermark ReadBatch() error = %v", err)
	}
	if len(watermarkBatch.Rows) != 2 || watermark.UpperBound() == nil {
		t.Fatalf("watermark result = rows:%#v upper:%#v", watermarkBatch.Rows, watermark.UpperBound())
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
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, schemaName+"."+tableName).Scan(&exists); err != nil {
		t.Fatalf("query deleted table: %v", err)
	}
	if exists {
		t.Fatal("DeleteResource() left the gate table")
	}
}

func openGaussIntegrationConnInfo() plugin.ConnectionInfo {
	return plugin.ConnectionInfo{
		"host":     openGaussIntegrationEnv("ADDP_TEST_OPENGAUSS_HOST", "127.0.0.1"),
		"port":     openGaussIntegrationEnv("ADDP_TEST_OPENGAUSS_PORT", "5432"),
		"database": openGaussIntegrationEnv("ADDP_TEST_OPENGAUSS_DATABASE", "addp_opengauss_disposable"),
		"user":     openGaussIntegrationEnv("ADDP_TEST_OPENGAUSS_USER", "gaussdb"),
		"password": openGaussIntegrationEnv("ADDP_TEST_OPENGAUSS_PASSWORD", ""),
		"sslmode":  "disable",
	}
}

func openGaussIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func findOpenGaussCatalogEntry(entries []plugin.EngineCatalogEntry, name string) *plugin.EngineCatalogEntry {
	for index := range entries {
		if strings.EqualFold(entries[index].Name, name) {
			return &entries[index]
		}
	}
	return nil
}

func catalogEntryNames(entries []plugin.EngineCatalogEntry) []string {
	names := make([]string, len(entries))
	for index := range entries {
		names[index] = entries[index].Name
	}
	return names
}

func assertOpenGaussField(t *testing.T, fields []datatype.FieldInfo, name string, fieldType datatype.FieldType, primaryKey bool) {
	t.Helper()
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) {
			if field.Type != fieldType || field.PrimaryKey != primaryKey {
				t.Fatalf("field %s = %#v, want type=%s primary_key=%v", name, field, fieldType, primaryKey)
			}
			return
		}
	}
	t.Fatalf("field %s not found in %#v", name, fields)
}

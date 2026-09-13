//go:build dameng_official

package dameng

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

func TestIntegrationDM8ProviderContract(t *testing.T) {
	if os.Getenv("ADDP_DAMENG_INTEGRATION") != "1" {
		t.Skip("set ADDP_DAMENG_INTEGRATION=1 to run DM8 integration test")
	}
	const engineID uint = 98001
	const schemaName = "ADDP_BUSINESS"
	const tableName = "ADDP_DM8_PROVIDER_CONTRACT"
	p := &Plugin{}
	connInfo := integrationConnInfo()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	if err := p.TestConnection(ctx, connInfo); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN() error = %v", err)
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	drop := func(cleanupCtx context.Context) error {
		var count int
		if err := db.QueryRowContext(cleanupCtx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE = 'TABLE' AND OBJECT_NAME = ?", tableName).Scan(&count); err != nil {
			return err
		}
		if count == 1 {
			_, err := db.ExecContext(cleanupCtx, `DROP TABLE "ADDP_BUSINESS"."ADDP_DM8_PROVIDER_CONTRACT"`)
			return err
		}
		return nil
	}
	if err := drop(ctx); err != nil {
		t.Fatalf("drop stale DM8 provider table: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := drop(cleanupCtx); err != nil {
			t.Errorf("cleanup DM8 provider table: %v", err)
		}
	})

	path := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermSchema, schemaName, tableName)
	fields := []datatype.FieldInfo{
		{Name: "ID", Type: datatype.FieldTypeBigInt, PrimaryKey: true, Nullable: false},
		{Name: "NAME", Type: datatype.FieldTypeString, Size: 128, Nullable: false},
		{Name: "AMOUNT", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2, Nullable: false},
		{Name: "ACTIVE", Type: datatype.FieldTypeBool, Nullable: false},
		{Name: "UPDATED_AT", Type: datatype.FieldTypeTimestamp, Nullable: false},
	}
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatalf("PrepareTableWrite() error = %v", err)
	}
	session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: fields})
	if err != nil {
		t.Fatalf("OpenTableWriteSession() error = %v", err)
	}
	baseTime := time.Date(2026, time.September, 13, 8, 0, 0, 123000000, time.UTC)
	initial := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{
		{"ID": int64(1), "NAME": "武汉", "AMOUNT": "10.25", "ACTIVE": true, "UPDATED_AT": baseTime},
		{"ID": int64(2), "NAME": "长沙", "AMOUNT": "20.50", "ACTIVE": false, "UPDATED_AT": baseTime.Add(time.Microsecond)},
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
		t.Fatalf("DM8 table write session does not provide a commit marker")
	}
	marker := markerProvider.CommitMarker()
	if marker == nil || marker.Provider != damengTableWriteSessionMarkerProvider {
		t.Fatalf("DM8 table write marker = %#v", marker)
	}

	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), engineID)
	schemas, err := p.ListChildren(ctx, connInfo, root, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	if len(schemas) != 1 || schemas[0].Name != schemaName {
		t.Fatalf("DM8 schemas = %#v", schemas)
	}
	tables, err := p.ListChildren(ctx, connInfo, schemas[0].Path, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(schema) error = %v", err)
	}
	var table *plugin.EngineCatalogEntry
	for index := range tables {
		if tables[index].Name == tableName {
			table = &tables[index]
		}
	}
	if table == nil {
		t.Fatalf("DM8 table %s not found", tableName)
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, table.Path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true, IncludeConstraints: true})
	if err != nil {
		t.Fatalf("DescribeEngineCatalogFacts() error = %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 2 || len(facts.Table.PrimaryKey) != 1 || facts.Table.PrimaryKey[0] != "ID" {
		t.Fatalf("DM8 facts = %#v", facts)
	}

	prepared, err := p.PrepareQuery(ctx, connInfo, plugin.QueryRequest{EngineID: engineID, Language: "sql", Query: `SELECT NAME, AMOUNT FROM "ADDP_BUSINESS"."ADDP_DM8_PROVIDER_CONTRACT" WHERE ID = :id`, Options: plugin.QueryOptions{ReadOnly: true, Parameters: map[string]interface{}{"id": int64(1)}}})
	if err != nil {
		t.Fatalf("PrepareQuery() error = %v", err)
	}
	result, err := prepared.Execute(ctx)
	if err != nil || len(result.Rows) != 1 || result.Rows[0]["NAME"] != "武汉" {
		t.Fatalf("PreparedQuery.Execute() rows=%#v error=%v", result.Rows, err)
	}

	queryPlan, err := p.PrepareQuery(ctx, connInfo, plugin.QueryRequest{EngineID: engineID, Language: "sql", Query: `SELECT ID, NAME FROM "ADDP_BUSINESS"."ADDP_DM8_PROVIDER_CONTRACT" ORDER BY ID`, Options: plugin.QueryOptions{ReadOnly: true}})
	if err != nil {
		t.Fatalf("PrepareQuery(session) error = %v", err)
	}
	querySession, err := p.OpenQueryReadSession(ctx, queryPlan)
	if err != nil {
		t.Fatalf("OpenQueryReadSession() error = %v", err)
	}
	queryBatch, err := querySession.ReadBatch(ctx, 10)
	if err != nil || len(queryBatch.Rows) != 2 {
		t.Fatalf("query session rows=%#v error=%v", queryBatch.Rows, err)
	}
	if err := querySession.Close(ctx); err != nil {
		t.Fatalf("query session Close() error = %v", err)
	}

	tableSession, err := p.OpenTableReadSession(ctx, connInfo, table.Path, plugin.TableReadSessionOptions{})
	if err != nil {
		t.Fatalf("OpenTableReadSession() error = %v", err)
	}
	tableBatch, err := tableSession.ReadBatch(ctx, 10)
	if err != nil || len(tableBatch.Rows) != 2 {
		t.Fatalf("table session rows=%#v error=%v", tableBatch.Rows, err)
	}
	if err := tableSession.Close(ctx); err != nil {
		t.Fatalf("table session Close() error = %v", err)
	}
	batch, err := p.ReadBatch(ctx, connInfo, table.Path, plugin.BatchReadOptions{Limit: 1, Offset: 1})
	if err != nil || len(batch.Rows) != 1 {
		t.Fatalf("ReadBatch() rows=%#v error=%v", batch.Rows, err)
	}

	watermark, err := p.OpenBoundedWatermarkRead(ctx, connInfo, table.Path, plugin.BoundedWatermarkReadOptions{WatermarkField: "UPDATED_AT", TieBreakers: []string{"ID"}})
	if err != nil {
		t.Fatalf("OpenBoundedWatermarkRead() error = %v", err)
	}
	watermarkBatch, err := watermark.ReadBatch(ctx, 10)
	if err != nil || len(watermarkBatch.Rows) != 2 || watermark.UpperBound() == nil {
		t.Fatalf("watermark rows=%#v upper=%#v error=%v", watermarkBatch.Rows, watermark.UpperBound(), err)
	}
	committed, err := watermark.PositionForRow(watermarkBatch.Rows[len(watermarkBatch.Rows)-1])
	if err != nil {
		t.Fatalf("PositionForRow() error = %v", err)
	}
	if err := watermark.Close(ctx); err != nil {
		t.Fatalf("watermark Close() error = %v", err)
	}

	upsertOpts := plugin.TableUpsertOptions{Fields: fields, Keys: []string{"ID"}}
	if err := p.PrepareTableUpsert(ctx, connInfo, table.Path, upsertOpts); err != nil {
		t.Fatalf("PrepareTableUpsert() error = %v", err)
	}
	changed := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{
		{"ID": int64(2), "NAME": "长沙更新", "AMOUNT": "21.00", "ACTIVE": true, "UPDATED_AT": baseTime.Add(2 * time.Microsecond)},
		{"ID": int64(3), "NAME": "西安", "AMOUNT": "30.75", "ACTIVE": true, "UPDATED_AT": baseTime.Add(3 * time.Microsecond)},
	}}
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.UpsertBatch(ctx, connInfo, table.Path, changed, upsertOpts); err != nil {
			t.Fatalf("UpsertBatch() attempt %d error = %v", attempt+1, err)
		}
	}
	resumeSession, err := p.OpenBoundedWatermarkRead(ctx, connInfo, table.Path, plugin.BoundedWatermarkReadOptions{WatermarkField: "UPDATED_AT", TieBreakers: []string{"ID"}, Start: committed})
	if err != nil {
		t.Fatalf("OpenBoundedWatermarkRead(resume) error = %v", err)
	}
	resumeBatch, err := resumeSession.ReadBatch(ctx, 10)
	if err != nil || len(resumeBatch.Rows) != 2 {
		t.Fatalf("resumed watermark rows=%#v error=%v", resumeBatch.Rows, err)
	}
	if err := resumeSession.Close(ctx); err != nil {
		t.Fatalf("resumed watermark Close() error = %v", err)
	}
	var rowCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "ADDP_BUSINESS"."ADDP_DM8_PROVIDER_CONTRACT"`).Scan(&rowCount); err != nil || rowCount != 3 {
		t.Fatalf("DM8 upsert row count=%d error=%v", rowCount, err)
	}
	if err := p.DeleteResource(ctx, connInfo, table.Path); err != nil {
		t.Fatalf("DeleteResource() error = %v", err)
	}
}

func integrationConnInfo() plugin.ConnectionInfo {
	port, _ := strconv.Atoi(envOrDefault("ADDP_TEST_DAMENG_PORT", "5236"))
	return plugin.ConnectionInfo{
		"host":     envOrDefault("ADDP_TEST_DAMENG_HOST", "business-dameng"),
		"port":     port,
		"user":     envOrDefault("ADDP_TEST_DAMENG_USER", "ADDP_BUSINESS"),
		"password": envOrDefault("ADDP_TEST_DAMENG_PASSWORD", "AddpBusiness8X"),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

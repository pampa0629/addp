package protection

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/testpg"
	_ "github.com/lib/pq"
)

func TestIntegrationPostgresBoundedExportMasksBeforeTargetWrite(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}

	ctx := context.Background()
	connInfo := testpg.ConnInfoFromEnv(t)
	pg := &postgresql.PostgreSQLPlugin{}
	dsn, err := pg.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	testpg.DropSchemasWithPrefixes(t, ctx, db, "transfer_security_test_")
	schema := fmt.Sprintf("transfer_security_test_%d", time.Now().UnixNano())
	testpg.CreateSchema(t, ctx, db, schema)
	sourceTable := "persons_source"
	targetTable := "persons_export"
	queryTargetTable := "persons_query_export"
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE "%s"."%s" (id integer, phone text);
		INSERT INTO "%s"."%s" (id, phone) VALUES (1, '13661384499');
	`, schema, sourceTable, schema, sourceTable)); err != nil {
		t.Fatalf("prepare PostgreSQL source: %v", err)
	}

	model := plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
	sourcePath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, schema, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, sourceTable)
	targetPath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, schema, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, targetTable)
	queryTargetPath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, schema, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, queryTargetTable)
	fields := []datatype.FieldInfo{
		{Name: "id", Path: []string{"id"}, Type: datatype.FieldTypeInt, Nullable: true},
		{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	store := transferProjectionStore(t)
	installActiveTransferProjection(t, store, model, sourcePath, fields)
	gate := NewGate(store, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "postgresql"}})
	protector, err := gate.PrepareBoundedTableProtection(ctx, 7, map[string]interface{}{
		"source": map[string]interface{}{"locator": fmt.Sprintf("addp://engine/11/path/%s/%s?type=table", schema, sourceTable)},
	})
	if err != nil {
		t.Fatal(err)
	}

	tableExecutor, err := executor.NewTableTransferExecutor("postgresql", "postgresql", "", "")
	if err != nil {
		t.Fatal(err)
	}
	tableExecutor.SourceProtector = protector
	metrics, err := tableExecutor.Execute(ctx, executor.TableTransferPlan{
		Source:    executor.TableSourcePlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: sourcePath, TableInfo: &datatype.TableInfo{Name: sourceTable, Fields: fields}},
		Target:    executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: targetPath},
		BatchSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
	var phone string
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT phone FROM "%s"."%s" WHERE id = 1`, schema, targetTable)).Scan(&phone); err != nil {
		t.Fatal(err)
	}
	if phone != "136****4499" {
		t.Fatalf("exported phone = %q, want masked value", phone)
	}

	queryExecutor, err := executor.NewTableTransferExecutor("postgresql", "postgresql", "", "")
	if err != nil {
		t.Fatal(err)
	}
	queryExecutor.SourceProtector = protector
	queryFields := []datatype.FieldInfo{
		{Name: "id", Path: []string{"id"}, Type: datatype.FieldTypeInt, Nullable: true},
		{Name: "contact_phone", Path: []string{"contact_phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	query := fmt.Sprintf(`SELECT id, phone AS contact_phone FROM "%s"."%s"`, schema, sourceTable)
	metrics, err = queryExecutor.Execute(ctx, executor.TableTransferPlan{
		Source: executor.TableSourcePlan{
			Kind: executor.TableEndpointQuery, ConnInfo: connInfo,
			RuntimeQuery: &plugin.QueryRequest{EngineID: 11, Language: "sql", Query: query, TargetPath: &sourcePath, Options: plugin.QueryOptions{ReadOnly: true}},
			TableInfo:    &datatype.TableInfo{Name: sourceTable, Fields: queryFields},
		},
		Target:    executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: queryTargetPath},
		BatchSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 {
		t.Fatalf("query metrics = %#v", metrics)
	}
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT contact_phone FROM "%s"."%s" WHERE id = 1`, schema, queryTargetTable)).Scan(&phone); err != nil {
		t.Fatal(err)
	}
	if phone != "136****4499" {
		t.Fatalf("query-exported phone = %q, want masked value", phone)
	}
}

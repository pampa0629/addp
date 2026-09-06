package executor

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	oracleengine "github.com/addp/common/engine/plugins/oracle"
	"github.com/addp/common/format"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
)

func TestIntegrationTransferBoundedOracleSpatialTarget(t *testing.T) {
	if os.Getenv("ADDP_TRANSFER_ORACLE_BOUNDED_E2E") != "1" {
		t.Skip("set ADDP_TRANSFER_ORACLE_BOUNDED_E2E=1 to run Oracle bounded transfer integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connInfo := engineplugin.ConnectionInfo{
		"host":         "127.0.0.1",
		"port":         15210,
		"service_name": "FREEPDB1",
		"user":         "business",
		"password":     "business_oracle_password",
	}
	schema := "BUSINESS"
	targetTable := "ADDP_TRANSFER_" + strings.ToUpper(uuid.NewString()[:8])
	sourcePath := engineplugin.TabularItemPath(22, engineplugin.EngineCatalogTermSchema, schema, "SPATIAL_FEATURES")
	targetPath := engineplugin.TabularItemPath(22, engineplugin.EngineCatalogTermSchema, schema, targetTable)
	oraclePlugin := &oracleengine.OraclePlugin{}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = oraclePlugin.DeleteResource(cleanupCtx, connInfo, targetPath)
	}()

	facts, err := oraclePlugin.DescribeEngineCatalogFacts(ctx, connInfo, sourcePath, engineplugin.EngineCatalogFactsOptions{IncludeSpatialFacts: true})
	if err != nil {
		t.Fatalf("DescribeEngineCatalogFacts source: %v", err)
	}
	if facts.Table == nil || facts.Spatial == nil {
		t.Fatalf("source facts=%#v", facts)
	}
	extent := datatype.NewBoundingBox(116.0, 39.0, 117.0, 41.0)
	facts.Spatial.Extent = &extent

	executor, err := NewTableTransferExecutor("oracle", "oracle", "", "")
	if err != nil {
		t.Fatalf("NewTableTransferExecutor: %v", err)
	}
	metrics, err := executor.Execute(ctx, TableTransferPlan{
		Source: TableSourcePlan{
			Kind: TableEndpointNative, ConnInfo: connInfo, Path: sourcePath,
			ReadOptions: map[string]interface{}{engineplugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB)},
			TableInfo:   facts.Table, SpatialInfo: facts.Spatial,
		},
		Target: TableTargetPlan{
			Kind: TableEndpointNative, ConnInfo: connInfo, Path: targetPath,
			DeleteBeforeWrite: true,
			TableWrite:        engineplugin.BatchWriteOptions{Method: "copy"},
		},
		BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if metrics.RecordsRead != 6 || metrics.RecordsWritten != 6 || metrics.Batches != 3 {
		t.Fatalf("metrics=%#v", metrics)
	}

	dsn, err := oraclePlugin.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	qualified := commonquery.ForDialect(commonquery.DialectOracle).QualifiedTable(schema, targetTable)
	var count int
	var minSRID, maxSRID int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*), MIN(target_row.\"SHAPE\".SDO_SRID), MAX(target_row.\"SHAPE\".SDO_SRID) FROM "+qualified+" target_row").Scan(&count, &minSRID, &maxSRID); err != nil {
		t.Fatal(err)
	}
	if count != 6 || minSRID != 4326 || maxSRID != 4326 {
		t.Fatalf("target rows=%d srid=%d/%d", count, minSRID, maxSRID)
	}
}

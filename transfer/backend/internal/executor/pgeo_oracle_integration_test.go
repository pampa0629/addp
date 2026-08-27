package executor

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	oracleengine "github.com/addp/common/engine/plugins/oracle"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	filegdbformat "github.com/addp/common/format/plugins/filegdb"
	pgeoformat "github.com/addp/common/format/plugins/pgeo"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
	"github.com/twpayne/go-geom/encoding/ewkb"
)

func TestIntegrationTransferPGeoToOracleSpatial(t *testing.T) {
	if os.Getenv("ADDP_TRANSFER_PGEO_ORACLE_BOUNDED_E2E") != "1" {
		t.Skip("set ADDP_TRANSFER_PGEO_ORACLE_BOUNDED_E2E=1 to run PGeo to Oracle Spatial integration test")
	}
	runPGeoOracleCase(t, pgeoIntegrationRequiredFixture(t, "ADDP_ARCGIS_PGEO_FIXTURE"), "WGS84_Points", "Shape", 265, 250, "Point", 4326)
}

func TestIntegrationTransferPGeoGeometryMatrixToOracleSpatial(t *testing.T) {
	if os.Getenv("ADDP_TRANSFER_PGEO_ORACLE_MATRIX_E2E") != "1" {
		t.Skip("set ADDP_TRANSFER_PGEO_ORACLE_MATRIX_E2E=1 to run PGeo geometry matrix integration test")
	}
	runPGeoOracleCase(t, pgeoIntegrationRequiredFixture(t, "ADDP_ARCGIS_PGEO_MATRIX_FIXTURE"), "Loess", "SHAPE", 123, 123, "MultiPolygon", 0)
}

func TestIntegrationTransferOracleSpatialToFileGDBRoundTrip(t *testing.T) {
	if os.Getenv("ADDP_TRANSFER_ORACLE_FILEGDB_ROUNDTRIP_E2E") != "1" {
		t.Skip("set ADDP_TRANSFER_ORACLE_FILEGDB_ROUNDTRIP_E2E=1 to run Oracle Spatial to FileGDB round-trip integration test")
	}

	fixture := pgeoIntegrationRequiredFixture(t, "ADDP_ARCGIS_PGEO_MATRIX_FIXTURE")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	target := executePGeoToOracle(t, ctx, fixture, "Loess", "SHAPE", 123)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := target.oraclePlugin.DeleteResource(cleanupCtx, target.oracleConn, target.path); err != nil {
			t.Errorf("delete Oracle round-trip source %s: %v", target.table, err)
		}
	}()

	runtime := plugin.NewHTTPWorkflowRuntimeProvider("geopython_workflow", "GeoPython Workflow")
	runtimeConn := plugin.ConnectionInfo{
		"protocol": pgeoIntegrationEnv("ADDP_TEST_GEOPYTHON_WORKFLOW_PROTOCOL", "http"),
		"host":     pgeoIntegrationEnv("ADDP_TEST_GEOPYTHON_WORKFLOW_HOST", "127.0.0.1"),
		"port":     pgeoIntegrationEnvInt(t, "ADDP_TEST_GEOPYTHON_WORKFLOW_PORT", 8099),
	}

	fileGDBName := "addp-oracle-roundtrip-" + strings.ToLower(uuid.NewString()[:8]) + ".gdb"
	targetRoot := strings.TrimSpace(os.Getenv("ADDP_ARCGIS_ROUNDTRIP_ROOT"))
	if targetRoot == "" {
		targetRoot = filepath.Dir(fixture)
	}
	fileGDBPath := filepath.Join(targetRoot, fileGDBName)
	t.Cleanup(func() {
		if err := os.RemoveAll(fileGDBPath); err != nil {
			t.Errorf("remove FileGDB round-trip target %s: %v", fileGDBPath, err)
		}
	})

	targetPlan, err := workflowaccess.NewTargetPlan(workflowaccess.Target{
		Kind: workflowaccess.KindDirectory, Format: "filegdb", Name: fileGDBName,
		WriteMode: workflowaccess.WriteModeReplace,
		Access:    workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: fileGDBPath},
	})
	if err != nil {
		t.Fatalf("build FileGDB target plan: %v", err)
	}
	targetScopeWriter, err := filegdbformat.NewPlugin().BindScopeTableWriter(runtime, runtimeConn, targetPlan)
	if err != nil {
		t.Fatalf("bind FileGDB target writer: %v", err)
	}

	facts, err := target.oraclePlugin.DescribeEngineCatalogFacts(ctx, target.oracleConn, target.path, plugin.EngineCatalogFactsOptions{IncludeSpatialFacts: true})
	if err != nil {
		t.Fatalf("describe Oracle round-trip source: %v", err)
	}
	if facts == nil || facts.Table == nil || facts.Spatial == nil {
		t.Fatalf("Oracle round-trip source facts = %#v, want table and spatial facts", facts)
	}

	executor := &TableTransferExecutor{
		SourceNativeReader:        target.oraclePlugin,
		TargetScopeWriterProvider: targetScopeWriter,
	}
	metrics, err := executor.Execute(ctx, TableTransferPlan{
		Source: TableSourcePlan{
			Kind: TableEndpointNative, ConnInfo: target.oracleConn, Path: target.path,
			TableInfo: facts.Table, SpatialInfo: facts.Spatial,
			ReadOptions: map[string]interface{}{
				plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
			},
		},
		Target: TableTargetPlan{
			Kind:   TableEndpointEncoded,
			Path:   plugin.FileDirectoryPath(14, "arcgis/"+fileGDBName),
			Format: format.FormatFileGDB, DeleteBeforeWrite: true,
			FormatOptions: &format.WriteOptions{ExtraParams: map[string]interface{}{"layer": "Loess"}},
		},
		BatchSize: 100,
	})
	if err != nil {
		t.Fatalf("execute Oracle Spatial to FileGDB: %v", err)
	}
	if metrics.RecordsRead != 123 || metrics.RecordsWritten != 123 || metrics.Batches != 2 {
		t.Fatalf("Oracle to FileGDB metrics=%#v, want 123 rows in 2 batches", metrics)
	}

	fileGDBSourcePlan, err := workflowaccess.NewSourcePlan(workflowaccess.Source{
		Kind: workflowaccess.KindDirectory, Format: "filegdb",
		Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: fileGDBPath},
	})
	if err != nil {
		t.Fatalf("build FileGDB round-trip source plan: %v", err)
	}
	sourceScopeReader, err := filegdbformat.NewPlugin().BindScopeTableReader(runtime, runtimeConn, fileGDBSourcePlan)
	if err != nil {
		t.Fatalf("bind FileGDB round-trip reader: %v", err)
	}
	parseOptions := format.DefaultParseOptions()
	parseOptions.GeometryEncoding = format.GeometryEncodingEWKB
	parseOptions.ExtraParams = map[string]interface{}{format.ChildNameParam: "Loess"}
	reader, err := sourceScopeReader.OpenTableScopeReader(ctx, nil, contentio.Ref{}, parseOptions)
	if err != nil {
		t.Fatalf("open FileGDB round-trip reader: %v", err)
	}
	defer reader.Close(context.Background())
	spatialReader, ok := reader.(format.TableSpatialInfoProvider)
	if !ok {
		t.Fatal("FileGDB round-trip reader does not expose spatial info")
	}
	spatial := spatialReader.SpatialInfo()
	if spatial == nil || spatial.PrimaryGeometryType() != "MultiPolygon" || spatial.PrimarySRIDValue() != 0 {
		t.Fatalf("FileGDB round-trip spatial facts = %#v, want MultiPolygon/SRID 0", spatial)
	}
	geometryColumn := spatial.PrimaryGeometryName()
	if geometryColumn == "" {
		t.Fatal("FileGDB round-trip has no primary geometry column")
	}
	rows := make([]map[string]interface{}, 0, 123)
	for {
		batch, readErr := reader.ReadRows(ctx, 100)
		if readErr != nil {
			t.Fatalf("read FileGDB round-trip rows: %v", readErr)
		}
		if len(batch) == 0 {
			break
		}
		rows = append(rows, batch...)
	}
	if len(rows) != 123 {
		t.Fatalf("FileGDB round-trip rows=%d, want 123", len(rows))
	}
	for index, row := range rows {
		value, ok := row[geometryColumn].([]byte)
		if !ok || len(value) == 0 {
			t.Fatalf("FileGDB round-trip row %d geometry=%#v, want non-empty EWKB", index, row[geometryColumn])
		}
		geometry, err := ewkb.Unmarshal(value)
		if err != nil {
			t.Fatalf("decode FileGDB round-trip row %d geometry: %v", index, err)
		}
		if geometry.SRID() != 0 {
			t.Fatalf("FileGDB round-trip row %d SRID=%d, want 0", index, geometry.SRID())
		}
	}
}

type pgeoOracleTarget struct {
	oraclePlugin *oracleengine.OraclePlugin
	oracleConn   plugin.ConnectionInfo
	schema       string
	table        string
	path         plugin.EngineCatalogPath
}

func runPGeoOracleCase(t *testing.T, fixture, child, geometryColumn string, expectedRows, expectedNonNull int, expectedGeometry string, expectedSRID int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	target := executePGeoToOracle(t, ctx, fixture, child, geometryColumn, expectedRows)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := target.oraclePlugin.DeleteResource(cleanupCtx, target.oracleConn, target.path); err != nil {
			t.Errorf("delete Oracle integration target %s: %v", target.table, err)
		}
	})

	dsn, err := target.oraclePlugin.BuildDSN(target.oracleConn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dialect := commonquery.ForEngine("oracle")
	qualified := dialect.QualifiedTable(target.schema, target.table)
	geometryExpression := "target_row." + dialect.QuoteIdentifier(geometryColumn)
	var rowCount, nonNullGeometry, minShapeType, maxShapeType int
	var minSRID, maxSRID sql.NullInt64
	query := fmt.Sprintf(`SELECT COUNT(*), COUNT(%[1]s),
		MIN(%[1]s.SDO_SRID), MAX(%[1]s.SDO_SRID),
		MIN(MOD(%[1]s.SDO_GTYPE, 1000)), MAX(MOD(%[1]s.SDO_GTYPE, 1000))
		FROM %[2]s target_row`, geometryExpression, qualified)
	if err := db.QueryRowContext(ctx, query).Scan(
		&rowCount, &nonNullGeometry, &minSRID, &maxSRID, &minShapeType, &maxShapeType,
	); err != nil {
		t.Fatalf("query Oracle Spatial target: %v", err)
	}
	if rowCount != expectedRows || nonNullGeometry != expectedNonNull {
		t.Fatalf("Oracle %s target rows=%d non_null_geometry=%d, want %d/%d", child, rowCount, nonNullGeometry, expectedRows, expectedNonNull)
	}
	if expectedSRID > 0 {
		if !minSRID.Valid || !maxSRID.Valid || int(minSRID.Int64) != expectedSRID || int(maxSRID.Int64) != expectedSRID {
			t.Fatalf("Oracle %s target SRID=%v/%v, want %d/%d", child, minSRID, maxSRID, expectedSRID, expectedSRID)
		}
	} else if minSRID.Valid || maxSRID.Valid {
		t.Fatalf("Oracle %s target SRID=%v/%v, want NULL/NULL", child, minSRID, maxSRID)
	}
	wantShapeType := map[string]int{"Point": 1, "MultiPolygon": 7}[expectedGeometry]
	if minShapeType != wantShapeType || maxShapeType != wantShapeType {
		t.Fatalf("Oracle %s target shape_type=%d/%d, want %s", child, minShapeType, maxShapeType, expectedGeometry)
	}
}

func executePGeoToOracle(t *testing.T, ctx context.Context, fixture, child, geometryColumn string, expectedRows int) pgeoOracleTarget {
	t.Helper()
	runtime := plugin.NewHTTPWorkflowRuntimeProvider("geopython_workflow", "GeoPython Workflow")
	runtimeConn := plugin.ConnectionInfo{
		"protocol": pgeoIntegrationEnv("ADDP_TEST_GEOPYTHON_WORKFLOW_PROTOCOL", "http"),
		"host":     pgeoIntegrationEnv("ADDP_TEST_GEOPYTHON_WORKFLOW_HOST", "127.0.0.1"),
		"port":     pgeoIntegrationEnvInt(t, "ADDP_TEST_GEOPYTHON_WORKFLOW_PORT", 8099),
	}
	sourcePlan, err := workflowaccess.NewSourcePlan(workflowaccess.Source{
		Kind: workflowaccess.KindFile, Format: string(format.FormatPGeo),
		Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: fixture},
	})
	if err != nil {
		t.Fatalf("build PGeo source plan: %v", err)
	}
	sourceProvider, err := pgeoformat.NewPlugin().BindScopeTableReader(runtime, runtimeConn, sourcePlan)
	if err != nil {
		t.Fatalf("bind PGeo source provider: %v", err)
	}
	oraclePlugin := &oracleengine.OraclePlugin{}
	oracleConn := plugin.ConnectionInfo{
		"host":         pgeoIntegrationEnv("ADDP_TEST_ORACLE_HOST", "127.0.0.1"),
		"port":         pgeoIntegrationEnvInt(t, "ADDP_TEST_ORACLE_PORT", 15210),
		"service_name": pgeoIntegrationEnv("ADDP_TEST_ORACLE_SERVICE_NAME", "FREEPDB1"),
		"user":         pgeoIntegrationEnv("ADDP_TEST_ORACLE_USER", "business"),
		"password":     pgeoIntegrationEnv("ADDP_TEST_ORACLE_PASSWORD", "business_oracle_password"),
	}
	targetSchema := pgeoIntegrationEnv("ADDP_TEST_ORACLE_SCHEMA", "BUSINESS")
	targetTable := "ADDP_PGEO_" + strings.ToUpper(uuid.NewString()[:8])
	targetPath := plugin.TabularItemPath(22, plugin.EngineCatalogTermSchema, targetSchema, targetTable)
	parseOptions := format.DefaultParseOptions()
	parseOptions.GeometryEncoding = format.GeometryEncodingEWKB
	parseOptions.ExtraParams = map[string]interface{}{format.ChildNameParam: child}
	executor := &TableTransferExecutor{
		SourceContentReader: &nfs.NFSPlugin{}, SourceScopeReadProvider: sourceProvider,
		TargetDeleteProvider: oraclePlugin, TargetNativePreparer: oraclePlugin,
		TargetTableSessionProvider: oraclePlugin,
	}
	metrics, err := executor.Execute(ctx, TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointEncoded, Path: plugin.FileItemPath(14, fixture), Format: format.FormatPGeo, Layout: format.LayoutSingle, ParseOptions: parseOptions},
		Target:    TableTargetPlan{Kind: TableEndpointNative, ConnInfo: oracleConn, Path: targetPath, DeleteBeforeWrite: true, TableWrite: plugin.BatchWriteOptions{Method: "copy"}},
		BatchSize: 100,
	})
	if err != nil {
		t.Fatalf("execute PGeo to Oracle Spatial: %v", err)
	}
	expectedBatches := (expectedRows + 99) / 100
	if metrics.RecordsRead != int64(expectedRows) || metrics.RecordsWritten != int64(expectedRows) || metrics.Batches != int64(expectedBatches) {
		t.Fatalf("%s transfer metrics=%#v, want %d rows in %d batches", child, metrics, expectedRows, expectedBatches)
	}
	return pgeoOracleTarget{oraclePlugin: oraclePlugin, oracleConn: oracleConn, schema: targetSchema, table: targetTable, path: targetPath}
}

func pgeoIntegrationRequiredFixture(t *testing.T, key string) string {
	t.Helper()
	fixture := strings.TrimSpace(os.Getenv(key))
	if fixture == "" {
		t.Fatalf("%s is required", key)
	}
	info, err := os.Stat(fixture)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("PGeo fixture is unavailable: %s: %v", fixture, err)
	}
	return fixture
}

func pgeoIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func pgeoIntegrationEnvInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	value := pgeoIntegrationEnv(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		t.Fatalf("invalid %s=%q", key, value)
	}
	return parsed
}

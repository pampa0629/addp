package executor

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	supermapworkflow "github.com/addp/common/engine/plugins/supermap_workflow"
	"github.com/addp/common/format"
	commonspatial "github.com/addp/common/spatial"
	_ "github.com/lib/pq"
	"github.com/twpayne/go-geom"
)

func TestIntegrationTransferPostGISAndSuperMapSDXPostgreSQL(t *testing.T) {
	if os.Getenv("ADDP_SUPERMAP_INTEGRATION") != "1" {
		t.Skip("set ADDP_SUPERMAP_INTEGRATION=1 to run PostGIS and SuperMap SDX+ for PostgreSQL integration test")
	}

	testCases := []struct {
		name         string
		geometryType string
		geometries   []string
	}{
		{
			name:         "polygon",
			geometryType: "Polygon",
			geometries: []string{
				"POLYGON((116.30 39.80,116.50 39.80,116.50 40.00,116.30 40.00,116.30 39.80))",
				"POLYGON((121.30 31.10,121.60 31.10,121.60 31.40,121.30 31.40,121.30 31.10))",
			},
		},
		{
			name:         "multipolygon",
			geometryType: "MultiPolygon",
			geometries: []string{
				"MULTIPOLYGON(((116.30 39.80,116.40 39.80,116.40 39.90,116.30 39.90,116.30 39.80)),((116.45 39.95,116.55 39.95,116.55 40.05,116.45 40.05,116.45 39.95)))",
				"MULTIPOLYGON(((121.30 31.10,121.40 31.10,121.40 31.20,121.30 31.20,121.30 31.10)),((121.45 31.25,121.55 31.25,121.55 31.35,121.45 31.35,121.45 31.25)))",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			runSuperMapTransferRoundTrip(t, testCase.geometryType, testCase.geometries)
		})
	}
}

func TestIntegrationSuperMapReplaceAbortDeletesTargetAfterCloseFailure(t *testing.T) {
	if os.Getenv("ADDP_SUPERMAP_INTEGRATION") != "1" {
		t.Skip("set ADDP_SUPERMAP_INTEGRATION=1 to run SuperMap replace cleanup integration test")
	}

	ctx := context.Background()
	realRuntime := superMapIntegrationRuntimeConnInfo(t)
	realRuntimeURL, err := url.Parse(fmt.Sprintf("%s://%s:%d", realRuntime["protocol"], realRuntime["host"], realRuntime["port"]))
	if err != nil {
		t.Fatalf("parse SuperMap Workflow runtime URL: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(realRuntimeURL)
	closeFailingRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/operators/table.write_close/invoke" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"failed","error":"injected close verification failure"}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	defer closeFailingRuntime.Close()
	proxyURL, err := url.Parse(closeFailingRuntime.URL)
	if err != nil {
		t.Fatalf("parse close-failing proxy URL: %v", err)
	}
	proxyPort, err := strconv.Atoi(proxyURL.Port())
	if err != nil {
		t.Fatalf("parse close-failing proxy port: %v", err)
	}

	provider, err := supermapworkflow.NewSDXPostgreSQLTableProvider(
		plugin.NewHTTPWorkflowRuntimeProvider("supermap_workflow", "SuperMap Workflow"),
		plugin.ConnectionInfo{"protocol": proxyURL.Scheme, "host": proxyURL.Hostname(), "port": proxyPort},
	)
	if err != nil {
		t.Fatalf("create close-failing SuperMap provider: %v", err)
	}
	databaseConn := superMapIntegrationDatabaseConnInfo(t)
	hostDatabaseConn := superMapIntegrationHostDatabaseConnInfo(t)
	pg := &postgresql.PostgreSQLPlugin{}
	db := openSuperMapIntegrationPostgres(t, ctx, pg, hostDatabaseConn)
	table := fmt.Sprintf("addp_transfer_abort_%d", time.Now().UnixNano())
	path := tableCatalogPath("sdx", table)
	t.Cleanup(func() { _ = provider.DeleteResource(context.Background(), databaseConn, path) })

	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeInt},
		{Name: "shape", Type: datatype.FieldTypeGeometry, Nullable: true},
	}
	spatialInfo := datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4326, 2)
	if err := provider.PrepareTableWrite(ctx, databaseConn, path, plugin.TableWriteOptions{Fields: fields, SpatialInfo: spatialInfo}); err != nil {
		t.Fatalf("prepare replace target: %v", err)
	}
	session, err := provider.OpenTableWriteSession(ctx, databaseConn, path, plugin.TableWriteSessionOptions{
		Fields: fields, SpatialInfo: spatialInfo, Replace: true,
	})
	if err != nil {
		t.Fatalf("open replace write session: %v", err)
	}
	ewkb, err := commonspatial.GeomToEWKB(geom.NewPointFlat(geom.XY, []float64{116.39, 39.90}), 4326)
	if err != nil {
		t.Fatalf("encode integration point EWKB: %v", err)
	}
	if err := session.WriteBatch(ctx, &plugin.BatchData{
		Fields:  fields,
		Rows:    []map[string]interface{}{{"id": 1, "shape": ewkb}},
		Spatial: spatialInfo,
	}); err != nil {
		t.Fatalf("write replace target batch: %v", err)
	}
	if err := session.Close(ctx); err == nil || !strings.Contains(err.Error(), "injected close verification failure") {
		t.Fatalf("Close() error = %v, want injected close verification failure", err)
	}
	if err := session.Abort(ctx); err != nil {
		t.Fatalf("abort replace session after close failure: %v", err)
	}

	var exists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'sdx' AND table_name = $1
		)
	`, table).Scan(&exists); err != nil {
		t.Fatalf("check aborted SuperMap target: %v", err)
	}
	if exists {
		t.Fatal("replace target still exists after close failure and abort")
	}
}

func TestIntegrationSuperMapCapacity(t *testing.T) {
	if os.Getenv("ADDP_SUPERMAP_CAPACITY") != "1" {
		t.Skip("set ADDP_SUPERMAP_CAPACITY=1 to run SuperMap capacity integration test")
	}

	rowCount := integrationEnvInt(t, "ADDP_TEST_SUPERMAP_CAPACITY_ROWS", 20000)
	batchSize := integrationEnvInt(t, "ADDP_TEST_SUPERMAP_CAPACITY_BATCH_SIZE", 1000)
	testCases := []struct {
		name               string
		geometryType       string
		geometryExpression string
	}{
		{
			name:         "polygon",
			geometryType: "Polygon",
			geometryExpression: `ST_MakeEnvelope(
				116.0 + (g % 1000) * 0.0001,
				39.0 + (g / 1000) * 0.0001,
				116.00004 + (g % 1000) * 0.0001,
				39.00004 + (g / 1000) * 0.0001,
				4326
			)::geometry(Polygon, 4326)`,
		},
		{
			name:         "multipolygon",
			geometryType: "MultiPolygon",
			geometryExpression: `ST_Multi(ST_Collect(
				ST_MakeEnvelope(
					116.0 + (g % 1000) * 0.0001,
					39.0 + (g / 1000) * 0.0001,
					116.00003 + (g % 1000) * 0.0001,
					39.00003 + (g / 1000) * 0.0001,
					4326
				),
				ST_MakeEnvelope(
					116.00005 + (g % 1000) * 0.0001,
					39.00005 + (g / 1000) * 0.0001,
					116.00008 + (g % 1000) * 0.0001,
					39.00008 + (g / 1000) * 0.0001,
					4326
				)
			))::geometry(MultiPolygon, 4326)`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			runSuperMapCapacityRoundTrip(t, testCase.geometryType, testCase.geometryExpression, rowCount, batchSize)
		})
	}
}

func runSuperMapCapacityRoundTrip(t *testing.T, geometryType, geometryExpression string, rowCount, batchSize int) {
	t.Helper()
	ctx := context.Background()
	pg := &postgresql.PostgreSQLPlugin{}
	postGISConn := superMapIntegrationPostGISConnInfo(t)
	postGISDB := openSuperMapIntegrationPostgres(t, ctx, pg, postGISConn)
	superMapDBConn := superMapIntegrationDatabaseConnInfo(t)
	superMapDB := openSuperMapIntegrationPostgres(t, ctx, pg, superMapIntegrationHostDatabaseConnInfo(t))

	runtime := plugin.NewHTTPWorkflowRuntimeProvider("supermap_workflow", "SuperMap Workflow")
	superMapProvider, err := supermapworkflow.NewSDXPostgreSQLTableProvider(runtime, superMapIntegrationRuntimeConnInfo(t))
	if err != nil {
		t.Fatalf("create SuperMap table provider: %v", err)
	}

	suffix := fmt.Sprintf("capacity_%s_%d", strings.ToLower(geometryType), time.Now().UnixNano())
	schema := "transfer_supermap_" + suffix
	sourceTable := "postgis_source"
	superMapTable := "addp_transfer_" + suffix
	roundTripTable := "postgis_roundtrip"
	superMapPath := tableCatalogPath("sdx", superMapTable)

	if _, err := postGISDB.ExecContext(ctx, `CREATE SCHEMA `+quoteIntegrationIdentifier(schema)); err != nil {
		t.Fatalf("create PostGIS capacity schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = postGISDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIntegrationIdentifier(schema)+` CASCADE`)
		_ = superMapProvider.DeleteResource(context.Background(), superMapDBConn, superMapPath)
	})

	createSourceSQL := fmt.Sprintf(
		`CREATE TABLE %s.%s (id integer PRIMARY KEY, name text NOT NULL, shape geometry(%s,4326) NOT NULL)`,
		quoteIntegrationIdentifier(schema),
		quoteIntegrationIdentifier(sourceTable),
		geometryType,
	)
	if _, err := postGISDB.ExecContext(ctx, createSourceSQL); err != nil {
		t.Fatalf("create PostGIS capacity source table: %v", err)
	}
	insertSourceSQL := fmt.Sprintf(
		`INSERT INTO %s.%s (id, name, shape)
		 SELECT g, 'feature-' || g, %s
		 FROM generate_series(1, $1) AS g`,
		quoteIntegrationIdentifier(schema),
		quoteIntegrationIdentifier(sourceTable),
		geometryExpression,
	)
	if _, err := postGISDB.ExecContext(ctx, insertSourceSQL, rowCount); err != nil {
		t.Fatalf("generate %d PostGIS capacity rows: %v", rowCount, err)
	}

	postGISToSuperMap := &TableTransferExecutor{
		SourceNativeReader:         pg,
		SourceTableSessionProvider: pg,
		TargetDeleteProvider:       superMapProvider,
		TargetNativePreparer:       superMapProvider,
		TargetTableSessionProvider: superMapProvider,
	}
	toSuperMapStartedAt := time.Now()
	toSuperMapMetrics, err := postGISToSuperMap.Execute(ctx, TableTransferPlan{
		Source: TableSourcePlan{
			Kind:     TableEndpointNative,
			ConnInfo: postGISConn,
			Path:     tableCatalogPath(schema, sourceTable),
			ReadOptions: map[string]interface{}{
				plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
				plugin.TableReadHintGeometryField:    "shape",
			},
		},
		Target: TableTargetPlan{
			Kind:              TableEndpointNative,
			ConnInfo:          superMapDBConn,
			Path:              superMapPath,
			DeleteBeforeWrite: true,
			TableWrite:        plugin.BatchWriteOptions{Method: "copy"},
		},
		BatchSize: batchSize,
	})
	if err != nil {
		t.Fatalf("execute %s capacity transfer to SuperMap: %v", geometryType, err)
	}
	toSuperMapElapsed := time.Since(toSuperMapStartedAt)
	assertIntegrationMetrics(t, toSuperMapMetrics, int64(rowCount))
	assertSuperMapPhysicalTable(t, ctx, superMapDB, superMapTable, int64(rowCount))

	superMapToPostGIS := &TableTransferExecutor{
		SourceTableSessionProvider: superMapProvider,
		TargetDeleteProvider:       pg,
		TargetNativePreparer:       pg,
		TargetNativeWriter:         pg,
		TargetTableSessionProvider: pg,
	}
	toPostGISStartedAt := time.Now()
	toPostGISMetrics, err := superMapToPostGIS.Execute(ctx, TableTransferPlan{
		Source: TableSourcePlan{
			Kind:     TableEndpointNative,
			ConnInfo: superMapDBConn,
			Path:     superMapPath,
		},
		Target: TableTargetPlan{
			Kind:              TableEndpointNative,
			ConnInfo:          postGISConn,
			Path:              tableCatalogPath(schema, roundTripTable),
			DeleteBeforeWrite: true,
			TableWrite:        plugin.BatchWriteOptions{Method: "copy"},
		},
		BatchSize: batchSize,
	})
	if err != nil {
		t.Fatalf("execute %s capacity transfer to PostGIS: %v", geometryType, err)
	}
	toPostGISElapsed := time.Since(toPostGISStartedAt)
	assertIntegrationMetrics(t, toPostGISMetrics, int64(rowCount))
	assertPostGISRoundTrip(t, ctx, postGISDB, schema, roundTripTable, geometryType, int64(rowCount))

	t.Logf(
		"%s rows=%d batch=%d PostGIS->SuperMap=%s (%.0f rows/s) SuperMap->PostGIS=%s (%.0f rows/s)",
		geometryType,
		rowCount,
		batchSize,
		toSuperMapElapsed.Round(time.Millisecond),
		float64(rowCount)/toSuperMapElapsed.Seconds(),
		toPostGISElapsed.Round(time.Millisecond),
		float64(rowCount)/toPostGISElapsed.Seconds(),
	)
}

func runSuperMapTransferRoundTrip(t *testing.T, geometryType string, geometries []string) {
	t.Helper()
	ctx := context.Background()
	pg := &postgresql.PostgreSQLPlugin{}
	postGISConn := superMapIntegrationPostGISConnInfo(t)
	postGISDB := openSuperMapIntegrationPostgres(t, ctx, pg, postGISConn)
	superMapDBConn := superMapIntegrationDatabaseConnInfo(t)
	superMapDB := openSuperMapIntegrationPostgres(t, ctx, pg, superMapIntegrationHostDatabaseConnInfo(t))

	runtime := plugin.NewHTTPWorkflowRuntimeProvider("supermap_workflow", "SuperMap Workflow")
	superMapProvider, err := supermapworkflow.NewSDXPostgreSQLTableProvider(runtime, superMapIntegrationRuntimeConnInfo(t))
	if err != nil {
		t.Fatalf("create SuperMap table provider: %v", err)
	}
	if err := superMapProvider.TestConnection(ctx, superMapDBConn); err != nil {
		t.Fatalf("test SuperMap Workflow connection: %v", err)
	}

	suffix := fmt.Sprintf("%s_%d", strings.ToLower(geometryType), time.Now().UnixNano())
	schema := "transfer_supermap_test_" + suffix
	sourceTable := "postgis_source"
	superMapTable := "addp_transfer_" + suffix
	roundTripTable := "postgis_roundtrip"
	superMapPath := tableCatalogPath("sdx", superMapTable)

	if _, err := postGISDB.ExecContext(ctx, `CREATE SCHEMA `+quoteIntegrationIdentifier(schema)); err != nil {
		t.Fatalf("create PostGIS test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = postGISDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIntegrationIdentifier(schema)+` CASCADE`)
		_ = superMapProvider.DeleteResource(context.Background(), superMapDBConn, superMapPath)
	})

	createSourceSQL := fmt.Sprintf(
		`CREATE TABLE %s.%s (id integer PRIMARY KEY, name text NOT NULL, shape geometry(%s,4326) NOT NULL)`,
		quoteIntegrationIdentifier(schema),
		quoteIntegrationIdentifier(sourceTable),
		geometryType,
	)
	if _, err := postGISDB.ExecContext(ctx, createSourceSQL); err != nil {
		t.Fatalf("create PostGIS source table: %v", err)
	}
	for index, geometry := range geometries {
		if _, err := postGISDB.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s.%s (id, name, shape) VALUES ($1, $2, ST_GeomFromText($3, 4326))`, quoteIntegrationIdentifier(schema), quoteIntegrationIdentifier(sourceTable)),
			index+1, fmt.Sprintf("feature-%d", index+1), geometry,
		); err != nil {
			t.Fatalf("insert PostGIS source row %d: %v", index, err)
		}
	}

	postGISToSuperMap := &TableTransferExecutor{
		SourceNativeReader:         pg,
		SourceTableSessionProvider: pg,
		TargetDeleteProvider:       superMapProvider,
		TargetNativePreparer:       superMapProvider,
		TargetTableSessionProvider: superMapProvider,
	}
	toSuperMapPlan := TableTransferPlan{
		Source: TableSourcePlan{
			Kind:     TableEndpointNative,
			ConnInfo: postGISConn,
			Path:     tableCatalogPath(schema, sourceTable),
			ReadOptions: map[string]interface{}{
				plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
				plugin.TableReadHintGeometryField:    "shape",
			},
		},
		Target: TableTargetPlan{
			Kind:              TableEndpointNative,
			ConnInfo:          superMapDBConn,
			Path:              superMapPath,
			DeleteBeforeWrite: true,
			TableWrite:        plugin.BatchWriteOptions{Method: "copy"},
		},
		BatchSize: 1,
	}
	metrics, err := postGISToSuperMap.Execute(ctx, toSuperMapPlan)
	if err != nil {
		t.Fatalf("execute PostGIS to SuperMap transfer: %v", err)
	}
	assertIntegrationMetrics(t, metrics, int64(len(geometries)))

	toSuperMapPlan.Target.DeleteBeforeWrite = false
	metrics, err = postGISToSuperMap.Execute(ctx, toSuperMapPlan)
	if err != nil {
		t.Fatalf("append PostGIS rows to SuperMap: %v", err)
	}
	assertIntegrationMetrics(t, metrics, int64(len(geometries)))

	assertSuperMapPhysicalTable(t, ctx, superMapDB, superMapTable, int64(len(geometries)*2))
	facts, err := superMapProvider.DescribeEngineCatalogFacts(ctx, superMapDBConn, superMapPath, plugin.EngineCatalogFactsOptions{
		IncludeSpatialFacts: true,
		IncludeStatistics:   true,
	})
	if err != nil {
		t.Fatalf("describe SuperMap SDK catalog facts: %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != int64(len(geometries)*2) {
		t.Fatalf("SuperMap SDK table facts = %#v, want row_count=%d", facts.Table, len(geometries)*2)
	}
	if len(facts.Table.Fields) != 3 || facts.Table.Fields[2].Name != "SmGeometry" || facts.Table.Fields[2].Type != datatype.FieldTypeGeometry {
		t.Fatalf("SuperMap SDK fields = %#v, want virtual SmGeometry", facts.Table.Fields)
	}
	if facts.Spatial == nil || facts.Spatial.PrimaryGeometryColumn != "SmGeometry" || facts.Spatial.HasSpatialIndex == nil || !*facts.Spatial.HasSpatialIndex {
		t.Fatalf("SuperMap SDK spatial facts = %#v, want indexed SmGeometry", facts.Spatial)
	}

	superMapToPostGIS := &TableTransferExecutor{
		SourceTableSessionProvider: superMapProvider,
		TargetDeleteProvider:       pg,
		TargetNativePreparer:       pg,
		TargetNativeWriter:         pg,
		TargetTableSessionProvider: pg,
	}
	metrics, err = superMapToPostGIS.Execute(ctx, TableTransferPlan{
		Source: TableSourcePlan{
			Kind:     TableEndpointNative,
			ConnInfo: superMapDBConn,
			Path:     superMapPath,
		},
		Target: TableTargetPlan{
			Kind:              TableEndpointNative,
			ConnInfo:          postGISConn,
			Path:              tableCatalogPath(schema, roundTripTable),
			DeleteBeforeWrite: true,
			TableWrite:        plugin.BatchWriteOptions{Method: "copy"},
		},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("execute SuperMap to PostGIS transfer: %v", err)
	}
	assertIntegrationMetrics(t, metrics, int64(len(geometries)*2))
	assertPostGISRoundTrip(t, ctx, postGISDB, schema, roundTripTable, geometryType, int64(len(geometries)*2))
}

func assertIntegrationMetrics(t *testing.T, metrics *TablePipelineMetrics, expectedRows int64) {
	t.Helper()
	if metrics == nil || metrics.RecordsRead != expectedRows || metrics.RecordsWritten != expectedRows {
		t.Fatalf("metrics = %#v, want %d rows read and written", metrics, expectedRows)
	}
}

func assertSuperMapPhysicalTable(t *testing.T, ctx context.Context, db *sql.DB, table string, expectedRows int64) {
	t.Helper()
	var dataType string
	if err := db.QueryRowContext(ctx, `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = 'sdx' AND table_name = $1 AND lower(column_name) = 'smgeometry'
	`, table).Scan(&dataType); err != nil {
		t.Fatalf("read SuperMap private geometry column: %v", err)
	}
	if dataType != "bytea" {
		t.Fatalf("SuperMap SmGeometry data type = %q, want bytea", dataType)
	}

	var rowCount int64
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s`, quoteIntegrationIdentifier("sdx"), quoteIntegrationIdentifier(table))).Scan(&rowCount); err != nil {
		t.Fatalf("count SuperMap physical rows: %v", err)
	}
	if rowCount != expectedRows {
		t.Fatalf("SuperMap physical row count = %d, want %d", rowCount, expectedRows)
	}

	var indexCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'sdx' AND tablename = $1 AND indexname LIKE 'sm_idx_%'`, table).Scan(&indexCount); err != nil {
		t.Fatalf("count SuperMap spatial indexes: %v", err)
	}
	if indexCount == 0 {
		t.Fatal("SuperMap physical table has no spatial index")
	}
}

func assertPostGISRoundTrip(t *testing.T, ctx context.Context, db *sql.DB, schema, table, geometryType string, expectedRows int64) {
	t.Helper()
	query := fmt.Sprintf(`
		SELECT COUNT(*), COUNT(*) FILTER (WHERE ST_IsValid(%[1]s)),
		       MIN(ST_SRID(%[1]s)), MAX(ST_SRID(%[1]s)),
		       MIN(GeometryType(%[1]s)), MAX(GeometryType(%[1]s))
		FROM %[2]s.%[3]s
	`, quoteIntegrationIdentifier("SmGeometry"), quoteIntegrationIdentifier(schema), quoteIntegrationIdentifier(table))
	var rowCount, validCount int64
	var minSRID, maxSRID int
	var minType, maxType string
	if err := db.QueryRowContext(ctx, query).Scan(&rowCount, &validCount, &minSRID, &maxSRID, &minType, &maxType); err != nil {
		t.Fatalf("verify PostGIS round-trip rows: %v", err)
	}
	if rowCount != expectedRows || validCount != expectedRows {
		t.Fatalf("PostGIS round-trip rows = %d, valid = %d, want %d", rowCount, validCount, expectedRows)
	}
	if minSRID != 4326 || maxSRID != 4326 {
		t.Fatalf("PostGIS round-trip SRID range = %d..%d, want 4326", minSRID, maxSRID)
	}
	expectedType := strings.ToUpper(geometryType)
	if minType != expectedType || maxType != expectedType {
		t.Fatalf("PostGIS round-trip geometry type range = %q..%q, want %q", minType, maxType, expectedType)
	}
}

func openSuperMapIntegrationPostgres(t *testing.T, ctx context.Context, pg *postgresql.PostgreSQLPlugin, connInfo plugin.ConnectionInfo) *sql.DB {
	t.Helper()
	dsn, err := pg.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("build PostgreSQL DSN: %v", err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func superMapIntegrationPostGISConnInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	return superMapIntegrationPostgresConnInfo(t, "ADDP_TEST_POSTGIS", "localhost", 5433, "business", "business_password", "business")
}

func superMapIntegrationHostDatabaseConnInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	return superMapIntegrationPostgresConnInfo(t, "ADDP_TEST_SUPERMAP_POSTGRESQL", "localhost", 5434, "supermap", "supermap_password", "supermap")
}

func superMapIntegrationDatabaseConnInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	return superMapIntegrationPostgresConnInfo(t, "ADDP_SUPERMAP_DATABASE", "host.docker.internal", 5434, "supermap", "supermap_password", "supermap")
}

func superMapIntegrationPostgresConnInfo(t *testing.T, prefix, defaultHost string, defaultPort int, defaultUser, defaultPassword, defaultDatabase string) plugin.ConnectionInfo {
	t.Helper()
	return plugin.ConnectionInfo{
		"host":     integrationEnv(prefix+"_HOST", defaultHost),
		"port":     integrationEnvInt(t, prefix+"_PORT", defaultPort),
		"user":     integrationEnv(prefix+"_USER", defaultUser),
		"password": integrationEnv(prefix+"_PASSWORD", defaultPassword),
		"database": integrationEnv(prefix+"_DATABASE", defaultDatabase),
		"sslmode":  integrationEnv(prefix+"_SSLMODE", "disable"),
	}
}

func superMapIntegrationRuntimeConnInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	return plugin.ConnectionInfo{
		"protocol": integrationEnv("ADDP_TEST_SUPERMAP_WORKFLOW_PROTOCOL", "http"),
		"host":     integrationEnv("ADDP_TEST_SUPERMAP_WORKFLOW_HOST", "localhost"),
		"port":     integrationEnvInt(t, "ADDP_TEST_SUPERMAP_WORKFLOW_PORT", 8103),
	}
}

func integrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func integrationEnvInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	value := integrationEnv(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		t.Fatalf("%s=%q must be a positive integer", key, value)
	}
	return parsed
}

func tableCatalogPath(schema, table string) plugin.EngineCatalogPath {
	return plugin.EngineCatalogPath{Segments: []plugin.EngineCatalogSegment{{Name: schema}, {Name: table}}}
}

func quoteIntegrationIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

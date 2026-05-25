package executor

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/addp/common/datatype"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/format"
	shapefileformat "github.com/addp/common/format/plugins/shapefile"
	"github.com/jonas-p/go-shp"
	_ "github.com/lib/pq"
)

func TestIntegrationShapefileToPostgresWritesEWKBGeometry(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL/PostGIS integration test")
	}

	ctx := context.Background()
	connInfo := integrationPostgresConnInfo()
	pg := &postgresql.PostgreSQLPlugin{}
	connStr, err := pg.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("open postgres failed: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "SELECT postgis_version()"); err != nil {
		t.Skipf("PostGIS is not available: %v", err)
	}

	source := &fakeContentWriter{files: map[string][]byte{}}
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	sourcePath := engineplugin.FileItemPath(0, "imports/cities.shp")
	contentWriter := contentadapter.NewWriter(source, nil, sourcePath, engineplugin.WriteOptions{Overwrite: true})
	refs := format.SameBasenameRelatedRefs(sourcePath.StringPath(), shapefilePlugin.RelatedRefSpecs())
	sourceTableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString, Size: 32},
			{Name: "geometry", Type: datatype.FieldTypeGeometry},
		},
	}
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(ctx, contentWriter, refs, sourceTableInfo, &format.WriteOptions{
		Encoding:    "utf-8",
		SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geometry", "Point", 4326, 0),
		ExtraParams: map[string]interface{}{
			"geometry_field":  "geometry",
			"spatial_ref_sys": "EPSG:4326",
		},
	})
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	if err := tableWriter.WriteRows(ctx, []map[string]interface{}{
		{"id": 1, "name": "Alpha", "geometry": "POINT (120 30)"},
		{"id": 2, "name": "Beta", "geometry": "POINT (121 31)"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := tableWriter.Close(ctx); err != nil {
		t.Fatalf("Close shapefile writer failed: %v", err)
	}

	schemaName := "transfer_it"
	tableName := fmt.Sprintf("shp_to_pg_%d", time.Now().UnixNano())
	targetPath := integrationPostgresTablePath(schemaName, tableName)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS "%s"."%s"`, schemaName, tableName))
	})

	parseOptions := format.DefaultParseOptions()
	parseOptions.GeometryEncoding = format.GeometryEncodingEWKB
	parseOptions.SpatialRefSys = "EPSG:4326"
	exec := &TableTransferExecutor{
		SourceContentReader:        source,
		SourceMultiReadProvider:    shapefilePlugin,
		TargetDeleteProvider:       pg,
		TargetNativePreparer:       pg,
		TargetNativeWriter:         pg,
		TargetTableSessionProvider: pg,
	}
	metrics, err := exec.Execute(ctx, TableTransferPlan{
		Source: TableSourcePlan{
			Kind:         TableEndpointEncoded,
			Path:         sourcePath,
			Format:       format.FormatShapefile,
			ParseOptions: parseOptions,
		},
		Target: TableTargetPlan{
			Kind:              TableEndpointNative,
			ConnInfo:          connInfo,
			Path:              targetPath,
			DeleteBeforeWrite: true,
			TableWrite:        engineplugin.BatchWriteOptions{Method: "copy"},
		},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 {
		t.Fatalf("metrics = %#v, want 2 read/written", metrics)
	}

	var rowCount int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."%s"`, schemaName, tableName)).Scan(&rowCount); err != nil {
		t.Fatalf("query row count failed: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("row count = %d, want 2", rowCount)
	}

	var geometryType string
	var srid int
	var wkt string
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT GeometryType("geometry"), ST_SRID("geometry"), ST_AsText("geometry") FROM "%s"."%s" WHERE "ID" = 1`, schemaName, tableName)).Scan(&geometryType, &srid, &wkt); err != nil {
		t.Fatalf("query geometry failed: %v", err)
	}
	if geometryType != "POINT" || srid != 4326 || wkt != "POINT(120 30)" {
		t.Fatalf("geometry = (%q, %d, %q), want POINT 4326 POINT(120 30)", geometryType, srid, wkt)
	}

	var columnType string
	var geometryColumnSRID int
	if err := db.QueryRowContext(ctx, `
		SELECT type, srid
		FROM geometry_columns
		WHERE f_table_schema = $1 AND f_table_name = $2 AND f_geometry_column = $3
	`, schemaName, tableName, "geometry").Scan(&columnType, &geometryColumnSRID); err != nil {
		t.Fatalf("query geometry_columns failed: %v", err)
	}
	if !strings.EqualFold(columnType, "POINT") || geometryColumnSRID != 4326 {
		t.Fatalf("geometry_columns = (%q, %d), want POINT 4326", columnType, geometryColumnSRID)
	}
}

func TestIntegrationShapefilePointZToPostgresPreservesZ(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL/PostGIS integration test")
	}

	ctx := context.Background()
	connInfo := integrationPostgresConnInfo()
	pg := &postgresql.PostgreSQLPlugin{}
	db := openIntegrationPostgres(t, ctx, pg, connInfo)
	defer db.Close()

	schemaName, tableName := runShapefileZToPostgres(t, ctx, db, pg, connInfo, "cities_z", shp.POINTZ, &shp.PointZ{X: 120, Y: 30, Z: 99.5, M: 0})

	var geometryType string
	var srid int
	var z float64
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT GeometryType("geometry"), ST_SRID("geometry"), ST_Z("geometry") FROM "%s"."%s" WHERE "ID" = 1`, schemaName, tableName)).Scan(&geometryType, &srid, &z); err != nil {
		t.Fatalf("query point z geometry failed: %v", err)
	}
	if geometryType != "POINT" || srid != 4326 || z != 99.5 {
		t.Fatalf("geometry = (%q, %d, %v), want POINT 4326 99.5", geometryType, srid, z)
	}

	var columnType string
	var coordinateDimension int
	if err := db.QueryRowContext(ctx, `
		SELECT type, coord_dimension
		FROM geometry_columns
		WHERE f_table_schema = $1 AND f_table_name = $2 AND f_geometry_column = $3
	`, schemaName, tableName, "geometry").Scan(&columnType, &coordinateDimension); err != nil {
		t.Fatalf("query geometry_columns failed: %v", err)
	}
	if !strings.EqualFold(columnType, "POINT") || coordinateDimension != 3 {
		t.Fatalf("geometry_columns = (%q, %d), want POINT dimension 3", columnType, coordinateDimension)
	}
}

func TestIntegrationShapefileComplexZToPostgresPreservesZ(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL/PostGIS integration test")
	}

	ctx := context.Background()
	connInfo := integrationPostgresConnInfo()
	pg := &postgresql.PostgreSQLPlugin{}
	db := openIntegrationPostgres(t, ctx, pg, connInfo)
	defer db.Close()

	tests := []struct {
		name           string
		sourceBaseName string
		shapeType      shp.ShapeType
		shape          shp.Shape
		query          string
		wantGeometry   string
		wantZ          float64
	}{
		{
			name:           "polyline z",
			sourceBaseName: "routes_z",
			shapeType:      shp.POLYLINEZ,
			shape: polyLineZShape([]int32{0}, []shp.Point{
				{X: 120, Y: 30},
				{X: 121, Y: 31},
			}, []float64{10.25, 11.5}),
			query:        `SELECT GeometryType("geometry"), ST_Z(ST_PointN("geometry", 2)) FROM "%s"."%s" WHERE "ID" = 1`,
			wantGeometry: "LINESTRING",
			wantZ:        11.5,
		},
		{
			name:           "polygon z",
			sourceBaseName: "areas_z",
			shapeType:      shp.POLYGONZ,
			shape: polygonZShape([]int32{0}, []shp.Point{
				{X: 0, Y: 0},
				{X: 0, Y: 1},
				{X: 1, Y: 1},
				{X: 1, Y: 0},
				{X: 0, Y: 0},
			}, []float64{20, 21, 22, 23, 20}),
			query:        `SELECT GeometryType("geometry"), ST_Z(ST_PointN(ST_ExteriorRing("geometry"), 3)) FROM "%s"."%s" WHERE "ID" = 1`,
			wantGeometry: "POLYGON",
			wantZ:        22,
		},
		{
			name:           "multipoint z",
			sourceBaseName: "sites_z",
			shapeType:      shp.MULTIPOINTZ,
			shape: &shp.MultiPointZ{
				Box:       shp.BBoxFromPoints([]shp.Point{{X: 120, Y: 30}, {X: 121, Y: 31}}),
				NumPoints: 2,
				Points:    []shp.Point{{X: 120, Y: 30}, {X: 121, Y: 31}},
				ZRange:    [2]float64{30.5, 31.5},
				ZArray:    []float64{30.5, 31.5},
				MRange:    [2]float64{0, 0},
				MArray:    []float64{0, 0},
			},
			query:        `SELECT GeometryType("geometry"), ST_Z(ST_GeometryN("geometry", 2)) FROM "%s"."%s" WHERE "ID" = 1`,
			wantGeometry: "MULTIPOINT",
			wantZ:        31.5,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			schemaName, tableName := runShapefileZToPostgres(t, ctx, db, pg, connInfo, tt.sourceBaseName, tt.shapeType, tt.shape)

			var geometryType string
			var z float64
			if err := db.QueryRowContext(ctx, fmt.Sprintf(tt.query, schemaName, tableName)).Scan(&geometryType, &z); err != nil {
				t.Fatalf("query z geometry failed: %v", err)
			}
			if geometryType != tt.wantGeometry || z != tt.wantZ {
				t.Fatalf("geometry = (%q, %v), want %s %v", geometryType, z, tt.wantGeometry, tt.wantZ)
			}

			var coordinateDimension int
			if err := db.QueryRowContext(ctx, `
				SELECT coord_dimension
				FROM geometry_columns
				WHERE f_table_schema = $1 AND f_table_name = $2 AND f_geometry_column = $3
			`, schemaName, tableName, "geometry").Scan(&coordinateDimension); err != nil {
				t.Fatalf("query geometry_columns failed: %v", err)
			}
			if coordinateDimension != 3 {
				t.Fatalf("coord_dimension = %d, want 3", coordinateDimension)
			}
		})
	}
}

func integrationPostgresConnInfo() engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"host":     integrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		"port":     integrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		"user":     integrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		"password": integrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		"database": integrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp"),
		"sslmode":  integrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	}
}

func openIntegrationPostgres(t *testing.T, ctx context.Context, pg *postgresql.PostgreSQLPlugin, connInfo engineplugin.ConnectionInfo) *sql.DB {
	t.Helper()

	connStr, err := pg.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("open postgres failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, "SELECT postgis_version()"); err != nil {
		_ = db.Close()
		t.Skipf("PostGIS is not available: %v", err)
	}
	return db
}

func integrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func integrationPostgresTablePath(schemaName, tableName string) engineplugin.CatalogPath {
	return engineplugin.CatalogPath{
		Version: engineplugin.CatalogPathVersion,
		Segments: []engineplugin.CatalogSegment{
			{Term: engineplugin.CatalogTermSchema, Kind: engineplugin.CatalogKindNamespace, Name: schemaName},
			{Term: engineplugin.CatalogTermTable, Kind: engineplugin.CatalogKindTable, Name: tableName},
		},
	}
}

func runShapefileZToPostgres(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	pg *postgresql.PostgreSQLPlugin,
	connInfo engineplugin.ConnectionInfo,
	sourceBaseName string,
	shapeType shp.ShapeType,
	shape shp.Shape,
) (string, string) {
	t.Helper()

	sourcePath := engineplugin.FileItemPath(0, "imports/"+sourceBaseName+".shp")
	source := shapefileContent(t, sourcePath.StringPath(), sourceBaseName, shapeType, shape)
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	schemaName := "transfer_it"
	tableName := fmt.Sprintf("%s_to_pg_%d", sourceBaseName, time.Now().UnixNano())
	targetPath := integrationPostgresTablePath(schemaName, tableName)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS "%s"."%s"`, schemaName, tableName))
	})

	parseOptions := format.DefaultParseOptions()
	parseOptions.GeometryEncoding = format.GeometryEncodingEWKB
	parseOptions.SpatialRefSys = "EPSG:4326"
	exec := &TableTransferExecutor{
		SourceContentReader:        source,
		SourceMultiReadProvider:    shapefilePlugin,
		TargetDeleteProvider:       pg,
		TargetNativePreparer:       pg,
		TargetNativeWriter:         pg,
		TargetTableSessionProvider: pg,
	}
	metrics, err := exec.Execute(ctx, TableTransferPlan{
		Source: TableSourcePlan{
			Kind:         TableEndpointEncoded,
			Path:         sourcePath,
			Format:       format.FormatShapefile,
			ParseOptions: parseOptions,
		},
		Target: TableTargetPlan{
			Kind:              TableEndpointNative,
			ConnInfo:          connInfo,
			Path:              targetPath,
			DeleteBeforeWrite: true,
			TableWrite:        engineplugin.BatchWriteOptions{Method: "copy"},
		},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 {
		t.Fatalf("metrics = %#v, want 1 read/written", metrics)
	}
	return schemaName, tableName
}

func shapefileContent(t *testing.T, sourcePath, baseName string, shapeType shp.ShapeType, shape shp.Shape) *fakeContentWriter {
	t.Helper()

	base := filepath.Join(t.TempDir(), baseName)
	writer, err := shp.Create(base+".shp", shapeType)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	if err := writer.SetFields([]shp.Field{
		shp.NumberField("ID", 8),
		shp.StringField("NAME", 16),
	}); err != nil {
		t.Fatalf("set shapefile fields failed: %v", err)
	}
	row := writer.Write(shape)
	if err := writer.WriteAttribute(int(row), 0, 1); err != nil {
		t.Fatalf("write id attribute failed: %v", err)
	}
	if err := writer.WriteAttribute(int(row), 1, "Alpha"); err != nil {
		t.Fatalf("write name attribute failed: %v", err)
	}
	writer.Close()
	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatalf("rename generated dbf failed: %v", err)
		}
	}

	files := map[string][]byte{}
	for _, ext := range []string{".shp", ".shx", ".dbf"} {
		data, err := os.ReadFile(base + ext)
		if err != nil {
			t.Fatalf("read generated shapefile ref %s failed: %v", ext, err)
		}
		refPath := strings.TrimSuffix(sourcePath, filepath.Ext(sourcePath)) + ext
		files[refPath] = data
	}
	return &fakeContentWriter{files: files}
}

func polyLineZShape(parts []int32, points []shp.Point, zArray []float64) *shp.PolyLineZ {
	return &shp.PolyLineZ{
		Box:       shp.BBoxFromPoints(points),
		NumParts:  int32(len(parts)),
		NumPoints: int32(len(points)),
		Parts:     parts,
		Points:    points,
		ZRange:    float64RangeArray(zArray),
		ZArray:    zArray,
		MRange:    [2]float64{0, 0},
		MArray:    make([]float64, len(points)),
	}
}

func polygonZShape(parts []int32, points []shp.Point, zArray []float64) *shp.PolygonZ {
	line := polyLineZShape(parts, points, zArray)
	polygon := shp.PolygonZ(*line)
	return &polygon
}

func float64RangeArray(values []float64) [2]float64 {
	if len(values) == 0 {
		return [2]float64{0, 0}
	}
	min, max := values[0], values[0]
	for _, value := range values[1:] {
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}
	return [2]float64{min, max}
}

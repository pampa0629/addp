package writers

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/readers"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// TestPostGISToShapefile tests data export from PostGIS to Shapefile
func TestPostGISToShapefile(t *testing.T) {
	// Skip if PostgreSQL not available
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		t.Skip("Skipping PostGIS test: TEST_POSTGRES_URL not set")
	}

	ctx := context.Background()
	connStr := os.Getenv("TEST_POSTGRES_URL")

	// 1. Setup: Create test table in PostgreSQL
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	// Ensure PostGIS extension exists
	_, err = db.Exec("CREATE EXTENSION IF NOT EXISTS postgis")
	if err != nil {
		t.Fatalf("Failed to create PostGIS extension: %v", err)
	}

	// Create test table
	_, err = db.Exec(`
		DROP TABLE IF EXISTS test_cities;
		CREATE TABLE test_cities (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100),
			population INTEGER,
			geom GEOMETRY(POINT, 4326)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Insert test data
	testData := []struct {
		name       string
		population int
		lon        float64
		lat        float64
	}{
		{"Beijing", 21540000, 116.4074, 39.9042},
		{"Shanghai", 24280000, 121.4737, 31.2304},
		{"Guangzhou", 15300000, 113.2644, 23.1291},
	}

	for _, city := range testData {
		_, err = db.Exec(`
			INSERT INTO test_cities (name, population, geom)
			VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326))
		`, city.name, city.population, city.lon, city.lat)
		if err != nil {
			t.Fatalf("Failed to insert test data: %v", err)
		}
	}

	// 2. Create Reader (PostgreSQL)
	readerConfig := pipeline.ConnectorConfig{
		Type: "postgresql",
		Config: map[string]interface{}{
			"connection_string": connStr,
			"query":             "SELECT id, name, population, ST_AsBinary(geom) as geom FROM test_cities ORDER BY id",
		},
		BatchSize: 100,
	}

	reader, err := readers.NewJDBCReader(readerConfig)
	if err != nil {
		t.Fatalf("Failed to create JDBC reader: %v", err)
	}

	err = reader.Open(ctx, readerConfig)
	if err != nil {
		t.Fatalf("Failed to open JDBC reader: %v", err)
	}
	defer reader.Close()

	// 3. Create Writer (Shapefile)
	tmpDir := t.TempDir()
	shpPath := filepath.Join(tmpDir, "test_cities.shp")

	writerConfig := pipeline.ConnectorConfig{
		Type: "shapefile",
		Config: map[string]interface{}{
			"file_path":      shpPath,
			"geometry_field": "geom",
		},
		BatchSize: 100,
	}

	writer, err := NewShapefileWriter(writerConfig)
	if err != nil {
		t.Fatalf("Failed to create Shapefile writer: %v", err)
	}

	err = writer.Open(ctx, writerConfig)
	if err != nil {
		t.Fatalf("Failed to open Shapefile writer: %v", err)
	}
	defer writer.Close()

	// 4. Transfer data
	for {
		batch, err := reader.Read(ctx)
		if err != nil {
			t.Fatalf("Failed to read batch: %v", err)
		}
		if batch.IsEmpty() {
			break
		}

		err = writer.Write(ctx, batch)
		if err != nil {
			t.Fatalf("Failed to write batch: %v", err)
		}
	}

	err = writer.Flush(ctx)
	if err != nil {
		t.Fatalf("Failed to flush writer: %v", err)
	}

	writer.Close()
	reader.Close()

	// 5. Verify: Read back Shapefile
	verifyConfig := pipeline.ConnectorConfig{
		Type: "shapefile",
		Config: map[string]interface{}{
			"file_path": shpPath,
		},
		BatchSize: 100,
	}

	verifyReader, err := readers.NewShapefileReader(verifyConfig)
	if err != nil {
		t.Fatalf("Failed to create verify reader: %v", err)
	}

	err = verifyReader.Open(ctx, verifyConfig)
	if err != nil {
		t.Fatalf("Failed to open verify reader: %v", err)
	}
	defer verifyReader.Close()

	batch, err := verifyReader.Read(ctx)
	if err != nil {
		t.Fatalf("Failed to read verify batch: %v", err)
	}

	if len(batch.Rows) != len(testData) {
		t.Errorf("Expected %d rows, got %d", len(testData), len(batch.Rows))
	}

	// Verify data integrity
	for i, row := range batch.Rows {
		if row["name"] != testData[i].name {
			t.Errorf("Row %d: expected name %s, got %v", i, testData[i].name, row["name"])
		}
		if row["geom"] == nil {
			t.Errorf("Row %d: geometry is nil", i)
		}
	}

	t.Logf("✅ Successfully transferred %d cities from PostGIS to Shapefile", len(batch.Rows))
}

// TestShapefileToPostGIS tests data import from Shapefile to PostGIS
func TestShapefileToPostGIS(t *testing.T) {
	// Skip if PostgreSQL not available
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		t.Skip("Skipping PostGIS test: TEST_POSTGRES_URL not set")
	}

	ctx := context.Background()
	connStr := os.Getenv("TEST_POSTGRES_URL")

	// 1. Setup: Create test Shapefile
	tmpDir := t.TempDir()
	shpPath := filepath.Join(tmpDir, "input.shp")

	// Create a simple Shapefile using writer
	writerConfig := pipeline.ConnectorConfig{
		Type: "shapefile",
		Config: map[string]interface{}{
			"file_path":      shpPath,
			"geometry_field": "geom",
		},
		BatchSize: 100,
	}

	writer, err := NewShapefileWriter(writerConfig)
	if err != nil {
		t.Fatalf("Failed to create Shapefile writer: %v", err)
	}

	err = writer.Open(ctx, writerConfig)
	if err != nil {
		t.Fatalf("Failed to open Shapefile writer: %v", err)
	}

	// Write test data (WKT format)
	testBatch := &pipeline.DataBatch{
		Rows: []map[string]interface{}{
			{
				"id":   1,
				"name": "Test Point 1",
				"geom": "POINT (100.0 50.0)",
			},
			{
				"id":   2,
				"name": "Test Point 2",
				"geom": "POINT (101.0 51.0)",
			},
		},
	}

	err = writer.Write(ctx, testBatch)
	if err != nil {
		t.Fatalf("Failed to write test Shapefile: %v", err)
	}

	writer.Flush(ctx)
	writer.Close()

	// 2. Setup: Create target table in PostgreSQL
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE EXTENSION IF NOT EXISTS postgis")
	if err != nil {
		t.Fatalf("Failed to create PostGIS extension: %v", err)
	}

	_, err = db.Exec(`
		DROP TABLE IF EXISTS test_import;
		CREATE TABLE test_import (
			id INTEGER,
			name VARCHAR(100),
			geom GEOMETRY(POINT, 4326)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create target table: %v", err)
	}

	// 3. Read from Shapefile
	readerConfig := pipeline.ConnectorConfig{
		Type: "shapefile",
		Config: map[string]interface{}{
			"file_path": shpPath,
		},
		BatchSize: 100,
	}

	reader, err := readers.NewShapefileReader(readerConfig)
	if err != nil {
		t.Fatalf("Failed to create Shapefile reader: %v", err)
	}

	err = reader.Open(ctx, readerConfig)
	if err != nil {
		t.Fatalf("Failed to open Shapefile reader: %v", err)
	}
	defer reader.Close()

	// 4. Write to PostgreSQL
	jdbcWriterConfig := pipeline.ConnectorConfig{
		Type: "postgresql",
		Config: map[string]interface{}{
			"connection_string": connStr,
			"table":             "test_import",
		},
		BatchSize: 100,
	}

	jdbcWriter, err := NewJDBCWriter(jdbcWriterConfig)
	if err != nil {
		t.Fatalf("Failed to create JDBC writer: %v", err)
	}

	err = jdbcWriter.Open(ctx, jdbcWriterConfig)
	if err != nil {
		t.Fatalf("Failed to open JDBC writer: %v", err)
	}
	defer jdbcWriter.Close()

	// Transfer data
	for {
		batch, err := reader.Read(ctx)
		if err != nil {
			t.Fatalf("Failed to read batch: %v", err)
		}
		if batch.IsEmpty() {
			break
		}

		err = jdbcWriter.Write(ctx, batch)
		if err != nil {
			t.Fatalf("Failed to write batch: %v", err)
		}
	}

	jdbcWriter.Flush(ctx)
	jdbcWriter.Close()
	reader.Close()

	// 5. Verify: Query PostgreSQL
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM test_import").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 rows in test_import, got %d", count)
	}

	// Verify geometry is valid
	var validCount int
	err = db.QueryRow("SELECT COUNT(*) FROM test_import WHERE ST_IsValid(geom)").Scan(&validCount)
	if err != nil {
		t.Fatalf("Failed to validate geometries: %v", err)
	}

	if validCount != count {
		t.Errorf("Expected all geometries to be valid, only %d/%d are valid", validCount, count)
	}

	t.Logf("✅ Successfully imported %d features from Shapefile to PostGIS", count)
}

// TestGeoPackagePostGISRoundTrip tests GeoPackage ⟷ PostGIS conversion
func TestGeoPackagePostGISRoundTrip(t *testing.T) {
	// Skip if PostgreSQL not available
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		t.Skip("Skipping PostGIS test: TEST_POSTGRES_URL not set")
	}

	ctx := context.Background()
	connStr := os.Getenv("TEST_POSTGRES_URL")

	// 1. Setup PostgreSQL test data
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE EXTENSION IF NOT EXISTS postgis")
	if err != nil {
		t.Fatalf("Failed to create PostGIS extension: %v", err)
	}

	_, err = db.Exec(`
		DROP TABLE IF EXISTS test_gpkg_source;
		CREATE TABLE test_gpkg_source (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100),
			geom GEOMETRY(POLYGON, 4326)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create source table: %v", err)
	}

	// Insert polygon test data
	_, err = db.Exec(`
		INSERT INTO test_gpkg_source (name, geom) VALUES
		('Polygon 1', ST_GeomFromText('POLYGON((0 0, 10 0, 10 10, 0 10, 0 0))', 4326)),
		('Polygon 2', ST_GeomFromText('POLYGON((20 20, 30 20, 30 30, 20 30, 20 20))', 4326))
	`)
	if err != nil {
		t.Fatalf("Failed to insert test polygons: %v", err)
	}

	// 2. Step 1: PostGIS → GeoPackage
	tmpDir := t.TempDir()
	gpkgPath := filepath.Join(tmpDir, "test.gpkg")

	// Read from PostGIS
	pgReaderConfig := pipeline.ConnectorConfig{
		Type: "postgresql",
		Config: map[string]interface{}{
			"connection_string": connStr,
			"query":             "SELECT id, name, ST_AsBinary(geom) as geom FROM test_gpkg_source ORDER BY id",
		},
		BatchSize: 100,
	}

	pgReader, err := readers.NewJDBCReader(pgReaderConfig)
	if err != nil {
		t.Fatalf("Failed to create PostgreSQL reader: %v", err)
	}

	err = pgReader.Open(ctx, pgReaderConfig)
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL reader: %v", err)
	}
	defer pgReader.Close()

	// Write to GeoPackage
	gpkgWriterConfig := pipeline.ConnectorConfig{
		Type: "geopackage",
		Config: map[string]interface{}{
			"file_path":      gpkgPath,
			"table":          "test_features",
			"geometry_field": "geom",
			"srid":           4326,
		},
		BatchSize: 100,
	}

	gpkgWriter, err := NewGeoPackageWriter(gpkgWriterConfig)
	if err != nil {
		t.Fatalf("Failed to create GeoPackage writer: %v", err)
	}

	err = gpkgWriter.Open(ctx, gpkgWriterConfig)
	if err != nil {
		t.Fatalf("Failed to open GeoPackage writer: %v", err)
	}

	for {
		batch, err := pgReader.Read(ctx)
		if err != nil {
			t.Fatalf("Failed to read from PostgreSQL: %v", err)
		}
		if batch.IsEmpty() {
			break
		}

		err = gpkgWriter.Write(ctx, batch)
		if err != nil {
			t.Fatalf("Failed to write to GeoPackage: %v", err)
		}
	}

	gpkgWriter.Flush(ctx)
	gpkgWriter.Close()
	pgReader.Close()

	t.Log("✅ Step 1 complete: PostGIS → GeoPackage")

	// 3. Step 2: GeoPackage → PostGIS (back)
	_, err = db.Exec(`
		DROP TABLE IF EXISTS test_gpkg_target;
		CREATE TABLE test_gpkg_target (
			id INTEGER,
			name VARCHAR(100),
			geom GEOMETRY(POLYGON, 4326)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create target table: %v", err)
	}

	// Read from GeoPackage
	gpkgReaderConfig := pipeline.ConnectorConfig{
		Type: "geopackage",
		Config: map[string]interface{}{
			"file_path": gpkgPath,
			"table":     "test_features",
		},
		BatchSize: 100,
	}

	gpkgReader, err := readers.NewGeoPackageReader(gpkgReaderConfig)
	if err != nil {
		t.Fatalf("Failed to create GeoPackage reader: %v", err)
	}

	err = gpkgReader.Open(ctx, gpkgReaderConfig)
	if err != nil {
		t.Fatalf("Failed to open GeoPackage reader: %v", err)
	}
	defer gpkgReader.Close()

	// Write to PostgreSQL
	pgWriterConfig := pipeline.ConnectorConfig{
		Type: "postgresql",
		Config: map[string]interface{}{
			"connection_string": connStr,
			"table":             "test_gpkg_target",
		},
		BatchSize: 100,
	}

	pgWriter, err := NewJDBCWriter(pgWriterConfig)
	if err != nil {
		t.Fatalf("Failed to create PostgreSQL writer: %v", err)
	}

	err = pgWriter.Open(ctx, pgWriterConfig)
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL writer: %v", err)
	}

	for {
		batch, err := gpkgReader.Read(ctx)
		if err != nil {
			t.Fatalf("Failed to read from GeoPackage: %v", err)
		}
		if batch.IsEmpty() {
			break
		}

		err = pgWriter.Write(ctx, batch)
		if err != nil {
			t.Fatalf("Failed to write to PostgreSQL: %v", err)
		}
	}

	pgWriter.Flush(ctx)
	pgWriter.Close()
	gpkgReader.Close()

	t.Log("✅ Step 2 complete: GeoPackage → PostGIS")

	// 4. Verify data integrity
	var sourceCount, targetCount int
	db.QueryRow("SELECT COUNT(*) FROM test_gpkg_source").Scan(&sourceCount)
	db.QueryRow("SELECT COUNT(*) FROM test_gpkg_target").Scan(&targetCount)

	if sourceCount != targetCount {
		t.Errorf("Row count mismatch: source=%d, target=%d", sourceCount, targetCount)
	}

	// Verify geometries are equivalent (using ST_Equals)
	var mismatchCount int
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM test_gpkg_source s
		JOIN test_gpkg_target t ON s.id = t.id
		WHERE NOT ST_Equals(s.geom, t.geom)
	`).Scan(&mismatchCount)

	if err != nil {
		t.Fatalf("Failed to verify geometry equality: %v", err)
	}

	if mismatchCount > 0 {
		t.Errorf("Found %d geometries that don't match after round-trip", mismatchCount)
	}

	t.Logf("✅ Round-trip test passed: %d features preserved correctly", sourceCount)
}

// TestMultiFormatPipeline tests complex multi-format conversion chain
func TestMultiFormatPipeline(t *testing.T) {
	// Skip if PostgreSQL not available
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		t.Skip("Skipping PostGIS test: TEST_POSTGRES_URL not set")
	}

	ctx := context.Background()
	connStr := os.Getenv("TEST_POSTGRES_URL")
	tmpDir := t.TempDir()

	// Test chain: PostgreSQL → Shapefile → GeoPackage → PostgreSQL
	// Using LineString geometries

	// 1. Setup: Create source data in PostgreSQL
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE EXTENSION IF NOT EXISTS postgis")
	if err != nil {
		t.Fatalf("Failed to create PostGIS extension: %v", err)
	}

	_, err = db.Exec(`
		DROP TABLE IF EXISTS test_pipeline_source;
		CREATE TABLE test_pipeline_source (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100),
			geom GEOMETRY(LINESTRING, 4326)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create source table: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO test_pipeline_source (name, geom) VALUES
		('Route A', ST_GeomFromText('LINESTRING(0 0, 1 1, 2 0)', 4326)),
		('Route B', ST_GeomFromText('LINESTRING(10 10, 11 11, 12 10)', 4326))
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	originalCount := 2
	t.Logf("📦 Source: PostgreSQL (%d LineStrings)", originalCount)

	// 2. Step 1: PostgreSQL → Shapefile
	shpPath := filepath.Join(tmpDir, "pipeline.shp")
	err = transferData(ctx, t,
		"postgresql", map[string]interface{}{
			"connection_string": connStr,
			"query":             "SELECT id, name, ST_AsBinary(geom) as geom FROM test_pipeline_source ORDER BY id",
		},
		"shapefile", map[string]interface{}{
			"file_path":      shpPath,
			"geometry_field": "geom",
		},
	)
	if err != nil {
		t.Fatalf("Step 1 failed (PostgreSQL → Shapefile): %v", err)
	}
	t.Log("✅ Step 1: PostgreSQL → Shapefile")

	// 3. Step 2: Shapefile → GeoPackage
	gpkgPath := filepath.Join(tmpDir, "pipeline.gpkg")
	err = transferData(ctx, t,
		"shapefile", map[string]interface{}{
			"file_path": shpPath,
		},
		"geopackage", map[string]interface{}{
			"file_path":      gpkgPath,
			"table":          "routes",
			"geometry_field": "geom",
		},
	)
	if err != nil {
		t.Fatalf("Step 2 failed (Shapefile → GeoPackage): %v", err)
	}
	t.Log("✅ Step 2: Shapefile → GeoPackage")

	// 4. Step 3: GeoPackage → PostgreSQL (new table)
	_, err = db.Exec(`
		DROP TABLE IF EXISTS test_pipeline_target;
		CREATE TABLE test_pipeline_target (
			id INTEGER,
			name VARCHAR(100),
			geom GEOMETRY(LINESTRING, 4326)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create target table: %v", err)
	}

	err = transferData(ctx, t,
		"geopackage", map[string]interface{}{
			"file_path": gpkgPath,
			"table":     "routes",
		},
		"postgresql", map[string]interface{}{
			"connection_string": connStr,
			"table":             "test_pipeline_target",
		},
	)
	if err != nil {
		t.Fatalf("Step 3 failed (GeoPackage → PostgreSQL): %v", err)
	}
	t.Log("✅ Step 3: GeoPackage → PostgreSQL")

	// 5. Verify final data
	var finalCount int
	err = db.QueryRow("SELECT COUNT(*) FROM test_pipeline_target").Scan(&finalCount)
	if err != nil {
		t.Fatalf("Failed to count final rows: %v", err)
	}

	if finalCount != originalCount {
		t.Errorf("Data loss in pipeline: started with %d, ended with %d", originalCount, finalCount)
	}

	// Verify geometry validity
	var validCount int
	err = db.QueryRow("SELECT COUNT(*) FROM test_pipeline_target WHERE ST_IsValid(geom)").Scan(&validCount)
	if err != nil {
		t.Fatalf("Failed to validate geometries: %v", err)
	}

	if validCount != finalCount {
		t.Errorf("Invalid geometries: %d/%d are valid", validCount, finalCount)
	}

	t.Logf("✅ Multi-format pipeline test passed: %d features transferred through PostgreSQL → Shapefile → GeoPackage → PostgreSQL", finalCount)
}

// Helper function to transfer data between two connectors
func transferData(ctx context.Context, t *testing.T, sourceType string, sourceConfig map[string]interface{}, targetType string, targetConfig map[string]interface{}) error {
	// Create reader
	readerConfig := pipeline.ConnectorConfig{
		Type:      sourceType,
		Config:    sourceConfig,
		BatchSize: 100,
	}

	var reader pipeline.Reader
	var err error

	switch sourceType {
	case "postgresql":
		reader, err = readers.NewJDBCReader(readerConfig)
	case "shapefile":
		reader, err = readers.NewShapefileReader(readerConfig)
	case "geopackage":
		reader, err = readers.NewGeoPackageReader(readerConfig)
	default:
		return fmt.Errorf("unknown source type: %s", sourceType)
	}

	if err != nil {
		return fmt.Errorf("failed to create reader: %w", err)
	}

	err = reader.Open(ctx, readerConfig)
	if err != nil {
		return fmt.Errorf("failed to open reader: %w", err)
	}
	defer reader.Close()

	// Create writer
	writerConfig := pipeline.ConnectorConfig{
		Type:      targetType,
		Config:    targetConfig,
		BatchSize: 100,
	}

	var writer pipeline.Writer

	switch targetType {
	case "postgresql":
		writer, err = NewJDBCWriter(writerConfig)
	case "shapefile":
		writer, err = NewShapefileWriter(writerConfig)
	case "geopackage":
		writer, err = NewGeoPackageWriter(writerConfig)
	default:
		return fmt.Errorf("unknown target type: %s", targetType)
	}

	if err != nil {
		return fmt.Errorf("failed to create writer: %w", err)
	}

	err = writer.Open(ctx, writerConfig)
	if err != nil {
		return fmt.Errorf("failed to open writer: %w", err)
	}
	defer writer.Close()

	// Transfer data
	for {
		batch, err := reader.Read(ctx)
		if err != nil {
			return fmt.Errorf("failed to read batch: %w", err)
		}
		if batch.IsEmpty() {
			break
		}

		err = writer.Write(ctx, batch)
		if err != nil {
			return fmt.Errorf("failed to write batch: %w", err)
		}
	}

	err = writer.Flush(ctx)
	if err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

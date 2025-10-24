# Integration Tests Guide

## Overview

This document describes the integration tests for the Transfer module's spatial data connectors.

## Test Coverage

The integration test suite (`internal/connector/integration_test.go`) includes:

1. **TestPostGISToShapefile** - Export from PostgreSQL/PostGIS to Shapefile format
2. **TestShapefileToPostGIS** - Import from Shapefile to PostgreSQL/PostGIS
3. **TestGeoPackagePostGISRoundTrip** - Round-trip test: PostGIS → GeoPackage → PostGIS
4. **TestMultiFormatPipeline** - Complex multi-format chain: PostgreSQL → Shapefile → GeoPackage → PostgreSQL

## Prerequisites

### 1. PostgreSQL with PostGIS Extension

You need a PostgreSQL database with PostGIS installed for integration testing.

**Option A: Use Docker (Recommended)**

```bash
# Start PostgreSQL with PostGIS
docker run -d \
  --name postgis-test \
  -e POSTGRES_PASSWORD=test_password \
  -e POSTGRES_USER=test_user \
  -e POSTGRES_DB=test_db \
  -p 5433:5432 \
  postgis/postgis:15-3.3

# Verify PostGIS is installed
docker exec -it postgis-test psql -U test_user -d test_db -c "CREATE EXTENSION IF NOT EXISTS postgis; SELECT PostGIS_Version();"
```

**Option B: Use Existing PostgreSQL**

If you already have PostgreSQL with PostGIS:

```sql
-- Connect to your database
CREATE EXTENSION IF NOT EXISTS postgis;

-- Verify installation
SELECT PostGIS_Version();
```

### 2. Set Environment Variable

Set the PostgreSQL connection URL:

```bash
export TEST_POSTGRES_URL="postgres://test_user:test_password@localhost:5433/test_db?sslmode=disable"
```

Or add to your shell profile (`~/.bashrc`, `~/.zshrc`):

```bash
# For integration tests
export TEST_POSTGRES_URL="postgres://test_user:test_password@localhost:5433/test_db?sslmode=disable"
```

## Running Tests

### Run All Integration Tests

```bash
cd /Users/pampa/code/addp/transfer/backend

# Run all integration tests
go test -v ./internal/connector/ -run "^Test.*PostGIS|Test.*GeoPackage|Test.*Pipeline"

# Or run with timeout (recommended for large datasets)
go test -v -timeout 5m ./internal/connector/
```

### Run Individual Tests

```bash
# Test PostGIS → Shapefile export
go test -v ./internal/connector/ -run TestPostGISToShapefile

# Test Shapefile → PostGIS import
go test -v ./internal/connector/ -run TestShapefileToPostGIS

# Test GeoPackage round-trip
go test -v ./internal/connector/ -run TestGeoPackagePostGISRoundTrip

# Test multi-format pipeline
go test -v ./internal/connector/ -run TestMultiFormatPipeline
```

### Skip Tests if PostgreSQL Not Available

If `TEST_POSTGRES_URL` is not set, tests will be automatically skipped:

```bash
$ go test -v ./internal/connector/ -run TestPostGISToShapefile
=== RUN   TestPostGISToShapefile
--- SKIP: TestPostGISToShapefile (0.00s)
    integration_test.go:21: Skipping PostGIS test: TEST_POSTGRES_URL not set
PASS
```

## Test Details

### TestPostGISToShapefile

**Purpose**: Verify data export from PostGIS to Shapefile format

**Test Flow**:
1. Create `test_cities` table in PostgreSQL with POINT geometries
2. Insert 3 test cities (Beijing, Shanghai, Guangzhou)
3. Read data using JDBCReader with `ST_AsBinary(geom)`
4. Write to Shapefile using ShapefileWriter
5. Verify: Read back Shapefile and check data integrity

**Expected Output**:
```
✅ Successfully transferred 3 cities from PostGIS to Shapefile
```

**What It Tests**:
- PostGIS GEOMETRY → WKB conversion
- Shapefile creation with correct schema
- Data integrity preservation

### TestShapefileToPostGIS

**Purpose**: Verify data import from Shapefile to PostGIS

**Test Flow**:
1. Create test Shapefile with 2 POINT features using WKT input
2. Create empty `test_import` table in PostgreSQL
3. Read from Shapefile using ShapefileReader
4. Write to PostgreSQL using JDBCWriter
5. Verify: Query PostgreSQL and validate geometries with `ST_IsValid()`

**Expected Output**:
```
✅ Successfully imported 2 features from Shapefile to PostGIS
```

**What It Tests**:
- Shapefile reading with schema inference
- WKB → PostGIS GEOMETRY conversion
- Geometry validation in target database

### TestGeoPackagePostGISRoundTrip

**Purpose**: Verify data fidelity in GeoPackage ⟷ PostGIS conversions

**Test Flow**:
1. Create `test_gpkg_source` table with POLYGON geometries in PostgreSQL
2. Insert 2 test polygons
3. **Step 1**: PostGIS → GeoPackage (using GeoPackageWriter)
4. **Step 2**: GeoPackage → PostGIS (new table `test_gpkg_target`)
5. Verify: Compare source and target using `ST_Equals()` for exact geometry match

**Expected Output**:
```
✅ Step 1 complete: PostGIS → GeoPackage
✅ Step 2 complete: GeoPackage → PostGIS
✅ Round-trip test passed: 2 features preserved correctly
```

**What It Tests**:
- GeoPackage file creation and metadata registration
- SQLite-based spatial data handling
- Geometry preservation through format conversions
- Data integrity using spatial equality checks

### TestMultiFormatPipeline

**Purpose**: Verify complex multi-format conversion chains

**Test Flow**:
1. Create `test_pipeline_source` with LINESTRING geometries in PostgreSQL
2. Insert 2 test routes
3. **Chain**: PostgreSQL → Shapefile → GeoPackage → PostgreSQL
4. Verify: Compare source and final target counts and geometry validity

**Expected Output**:
```
📦 Source: PostgreSQL (2 LineStrings)
✅ Step 1: PostgreSQL → Shapefile
✅ Step 2: Shapefile → GeoPackage
✅ Step 3: GeoPackage → PostgreSQL
✅ Multi-format pipeline test passed: 2 features transferred through ...
```

**What It Tests**:
- End-to-end format conversion chains
- Data loss detection across multiple transformations
- Geometry type preservation (LINESTRING through all formats)
- Connector interoperability

## Troubleshooting

### "connection refused" or "no such host"

**Problem**: Cannot connect to PostgreSQL

**Solution**:
```bash
# Check PostgreSQL is running
docker ps | grep postgis

# Check connection string
echo $TEST_POSTGRES_URL

# Test connection manually
psql "$TEST_POSTGRES_URL" -c "SELECT version();"
```

### "extension postgis does not exist"

**Problem**: PostGIS extension not installed

**Solution**:
```bash
# Install PostGIS in Docker container
docker exec -it postgis-test psql -U test_user -d test_db -c "CREATE EXTENSION postgis;"

# Or use postgis/postgis Docker image (includes PostGIS)
docker run -d --name postgis-test postgis/postgis:15-3.3
```

### "permission denied for schema public"

**Problem**: User lacks permissions

**Solution**:
```sql
-- Grant necessary permissions
GRANT CREATE ON SCHEMA public TO test_user;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO test_user;
```

### Tests timeout

**Problem**: Large dataset or slow I/O

**Solution**:
```bash
# Increase timeout
go test -v -timeout 10m ./internal/connector/
```

### Shapefile DBF encoding issues

**Problem**: Non-ASCII characters in field names

**Note**: Shapefile DBF format has limitations:
- Field names max 10 characters
- ASCII-only recommended
- Tests use simple ASCII names to avoid encoding issues

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Integration Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgis/postgis:15-3.3
        env:
          POSTGRES_PASSWORD: test_password
          POSTGRES_USER: test_user
          POSTGRES_DB: test_db
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v3

      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.23'

      - name: Run Integration Tests
        env:
          TEST_POSTGRES_URL: postgres://test_user:test_password@localhost:5432/test_db?sslmode=disable
        run: |
          cd transfer/backend
          go test -v -timeout 5m ./internal/connector/
```

## Adding New Integration Tests

### Template for New Tests

```go
func TestYourNewIntegration(t *testing.T) {
	// 1. Skip if dependencies not available
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		t.Skip("Skipping test: TEST_POSTGRES_URL not set")
	}

	ctx := context.Background()

	// 2. Setup test data
	// ...

	// 3. Create source connector
	sourceConfig := pipeline.ConnectorConfig{
		Type:   "your_source_type",
		Config: map[string]interface{}{ /* ... */ },
	}
	source, err := NewYourSourceConnector(sourceConfig)
	// ...

	// 4. Create target connector
	targetConfig := pipeline.ConnectorConfig{
		Type:   "your_target_type",
		Config: map[string]interface{}{ /* ... */ },
	}
	target, err := NewYourTargetConnector(targetConfig)
	// ...

	// 5. Transfer data
	err = transferData(ctx, t, sourceType, sourceConfig, targetType, targetConfig)
	if err != nil {
		t.Fatalf("Transfer failed: %v", err)
	}

	// 6. Verify results
	// ...

	t.Logf("✅ Test passed: ...")
}
```

### Best Practices

1. **Use temporary directories**: `tmpDir := t.TempDir()` for file-based tests
2. **Clean up resources**: Use `defer` for Close() calls
3. **Check data integrity**: Verify row counts, geometry validity, attribute values
4. **Use meaningful test data**: Real-world scenarios (cities, routes, polygons)
5. **Log progress**: Use `t.Log()` for debugging and visibility
6. **Skip gracefully**: Check environment variables and skip if dependencies unavailable

## Performance Benchmarks

### Benchmark Integration Tests

```bash
# Run benchmarks with memory profiling
go test -bench=. -benchmem ./internal/connector/

# Example output:
# BenchmarkPostGISToShapefile-8    50    24.5 ms/op    4.2 MB/op    850 allocs/op
```

### Adding Benchmarks

```go
func BenchmarkPostGISToShapefile(b *testing.B) {
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		b.Skip("Skipping benchmark: TEST_POSTGRES_URL not set")
	}

	ctx := context.Background()
	// Setup once
	// ...

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Benchmark code
		err := transferData(ctx, /* ... */)
		if err != nil {
			b.Fatalf("Transfer failed: %v", err)
		}
	}
}
```

## Test Data

All test data is automatically created and cleaned up by the tests. No external test files are required.

**Geometry Types Tested**:
- **POINT**: Cities with lat/lon coordinates
- **LINESTRING**: Routes with multiple vertices
- **POLYGON**: Areas with exterior rings
- **Future**: MULTIPOINT, MULTILINESTRING, MULTIPOLYGON, GEOMETRYCOLLECTION

**Spatial Reference Systems**:
- **SRID 4326**: WGS 84 (geographic coordinates)
- **Future**: SRID 3857 (Web Mercator), custom projections

## Next Steps

- [ ] Add tests for MULTIPOINT, MULTILINESTRING, MULTIPOLYGON geometries
- [ ] Add tests for 3D geometries (PointZ, LineStringZ, PolygonZ)
- [ ] Add tests for coordinate system transformations (after PROJ integration)
- [ ] Add tests for GeoJSON file format
- [ ] Add performance benchmarks for large datasets (10K+ features)
- [ ] Add tests for error handling (invalid geometries, schema mismatches)

## Related Documentation

- [SHORT_TERM_PLAN_COMPLETION.md](../SHORT_TERM_PLAN_COMPLETION.md) - Implementation status
- [POSTGIS_SHAPEFILE_GUIDE.md](../POSTGIS_SHAPEFILE_GUIDE.md) - PostGIS ⟷ Shapefile usage guide
- [SPATIAL_TRANSFORM_USAGE.md](../SPATIAL_TRANSFORM_USAGE.md) - Spatial transform API guide

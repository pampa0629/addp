package oracle

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
)

func TestIntegrationOracleTableWriteSessionSpatialRoundTrip(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_SPATIAL_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_SPATIAL_INTEGRATION=1 to run Oracle Spatial integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	schema := strings.ToUpper(connInfo["user"].(string))
	table := "ADDP_BOUNDED_" + strings.ToUpper(uuid.NewString()[:8])
	path := plugin.CatalogRootPath(p.CatalogModel(), 92001)
	path.Segments = append(path.Segments,
		plugin.CatalogSegment{Term: plugin.CatalogTermSchema, Kind: plugin.CatalogKindNamespace, Name: schema},
		plugin.CatalogSegment{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: table},
	)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = p.DeleteResource(cleanupCtx, connInfo, path)
	}()

	srid, dimension := 3424, 2
	extent := datatype.NewBoundingBox(193684.745025, 34945.754154, 657059.678181, 919549.443276)
	fields := []datatype.FieldInfo{
		{Name: "OBJECTID", Type: datatype.FieldTypeBigInt, Nullable: false, PrimaryKey: true},
		{Name: "COUNTY", Type: datatype.FieldTypeString, Nullable: false},
		{Name: "POP2010", Type: datatype.FieldTypeBigInt, Nullable: false},
		{Name: "SHAPE", Type: datatype.FieldTypeGeometry, Nullable: false},
	}
	spatialInfo := &datatype.SpatialInfo{
		GeometryColumns: []datatype.GeometryColumnInfo{{
			Name: "SHAPE", GeometryType: string(datatype.GeometryTypeMultiPolygon), SRID: &srid, Dimension: &dimension,
		}},
		PrimaryGeometryColumn: "SHAPE",
		Extent:                &extent,
	}
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields, SpatialInfo: spatialInfo}); err != nil {
		t.Fatalf("PrepareTableWrite: %v", err)
	}

	first := oracleIntegrationMultiPolygon(t, srid, 200000, 50000)
	second := oracleIntegrationMultiPolygon(t, srid, 300000, 150000)
	firstEWKB, err := ewkb.Marshal(first, ewkb.NDR)
	if err != nil {
		t.Fatal(err)
	}
	secondEWKB, err := ewkb.Marshal(second, ewkb.NDR)
	if err != nil {
		t.Fatal(err)
	}
	session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{
		Method: "copy", Fields: fields, SpatialInfo: spatialInfo,
	})
	if err != nil {
		t.Fatalf("OpenTableWriteSession: %v", err)
	}
	if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{
		{"OBJECTID": int64(1), "COUNTY": "Alpha", "POP2010": int64(100), "SHAPE": firstEWKB},
		{"OBJECTID": int64(2), "COUNTY": "Beta", "POP2010": int64(200), "SHAPE": secondEWKB},
	}}); err != nil {
		_ = session.Abort(ctx)
		t.Fatalf("WriteBatch: %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	markerProvider, ok := session.(plugin.CommitMarkerProvider)
	if !ok || markerProvider.CommitMarker() == nil {
		t.Fatal("Oracle table write session did not publish commit marker")
	}

	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	qualified := commonquery.ForEngine("oracle").QualifiedTable(schema, table)
	var count, population int64
	var minGType, maxGType, minSRID, maxSRID int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*), SUM(\"POP2010\"), MIN(target_row.\"SHAPE\".SDO_GTYPE), MAX(target_row.\"SHAPE\".SDO_GTYPE), MIN(target_row.\"SHAPE\".SDO_SRID), MAX(target_row.\"SHAPE\".SDO_SRID) FROM "+qualified+" target_row").Scan(
		&count, &population, &minGType, &maxGType, &minSRID, &maxSRID,
	); err != nil {
		t.Fatal(err)
	}
	if count != 2 || population != 300 || minGType != 2007 || maxGType != 2007 || minSRID != srid || maxSRID != srid {
		t.Fatalf("rows=%d population=%d gtype=%d/%d srid=%d/%d", count, population, minGType, maxGType, minSRID, maxSRID)
	}

	facts, err := p.DescribeCatalogFacts(ctx, connInfo, path, plugin.CatalogFactsOptions{IncludeSpatialFacts: true, IncludeIndexes: true})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts: %v", err)
	}
	if facts.Spatial == nil || facts.Spatial.PrimaryGeometryType() != string(datatype.GeometryTypeMultiPolygon) || facts.Spatial.PrimarySRIDValue() != srid || facts.Spatial.HasSpatialIndex == nil || !*facts.Spatial.HasSpatialIndex {
		t.Fatalf("spatial facts=%#v", facts.Spatial)
	}
	batch, err := readOracleIntegrationBatchAfterDDL(ctx, p, connInfo, path, plugin.BatchReadOptions{
		Limit: 10, Hints: map[string]interface{}{plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB)},
	})
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if len(batch.Rows) != 2 {
		t.Fatalf("round-trip rows=%d", len(batch.Rows))
	}
	for _, row := range batch.Rows {
		encoded, ok := row["SHAPE"].([]byte)
		if !ok {
			t.Fatalf("round-trip geometry=%T", row["SHAPE"])
		}
		geometry, err := ewkb.Unmarshal(encoded)
		if err != nil || geometry.SRID() != srid {
			t.Fatalf("round-trip geometry=%T srid=%d err=%v", geometry, geometry.SRID(), err)
		}
	}
}

func TestIntegrationOraclePrepareMixedCaseSpatialColumn(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_SPATIAL_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_SPATIAL_INTEGRATION=1 to run Oracle Spatial integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	schema := strings.ToUpper(connInfo["user"].(string))
	table := "ADDP_CASE_" + strings.ToUpper(uuid.NewString()[:8])
	path := plugin.CatalogRootPath(p.CatalogModel(), 92002)
	path.Segments = append(path.Segments,
		plugin.CatalogSegment{Term: plugin.CatalogTermSchema, Kind: plugin.CatalogKindNamespace, Name: schema},
		plugin.CatalogSegment{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: table},
	)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = p.DeleteResource(cleanupCtx, connInfo, path)
	}()

	srid, dimension := 4326, 2
	extent := datatype.NewBoundingBox(-180, -90, 180, 90)
	spatialInfo := &datatype.SpatialInfo{
		GeometryColumns:       []datatype.GeometryColumnInfo{{Name: "Shape", GeometryType: "Point", SRID: &srid, Dimension: &dimension}},
		PrimaryGeometryColumn: "Shape",
		Extent:                &extent,
	}
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "Shape", Type: datatype.FieldTypeGeometry},
	}
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields, SpatialInfo: spatialInfo}); err != nil {
		t.Fatalf("PrepareTableWrite: %v", err)
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var spatialIndexCount, metadataCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_indexes WHERE table_name = :1 AND index_type = 'DOMAIN'", table).Scan(&spatialIndexCount); err != nil {
		t.Fatal(err)
	}
	if spatialIndexCount != 1 {
		t.Fatalf("spatial index count = %d, want 1", spatialIndexCount)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_sdo_geom_metadata WHERE table_name = :1 AND column_name = :2", table, `"Shape"`).Scan(&metadataCount); err != nil {
		t.Fatal(err)
	}
	if metadataCount != 1 {
		t.Fatalf("quoted spatial metadata count = %d, want 1", metadataCount)
	}
	qualified := commonquery.ForEngine("oracle").QualifiedTable(schema, table)
	if _, err := db.ExecContext(ctx, `INSERT INTO `+qualified+` ("id", "Shape") VALUES (1, MDSYS.SDO_GEOMETRY(2001, 4326, MDSYS.SDO_POINT_TYPE(116.397, 39.908, NULL), NULL, NULL))`); err != nil {
		t.Fatalf("insert mixed-case spatial row: %v", err)
	}
	facts, err := p.DescribeCatalogFacts(ctx, connInfo, path, plugin.CatalogFactsOptions{IncludeSpatialFacts: true, IncludeIndexes: true})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts: %v", err)
	}
	if facts.Spatial == nil || facts.Spatial.PrimaryGeometryName() != "Shape" || facts.Spatial.PrimaryGeometryType() != "Point" || facts.Spatial.PrimarySRIDValue() != srid || facts.Spatial.PrimaryDimensionValue() != dimension {
		t.Fatalf("mixed-case spatial facts = %#v", facts.Spatial)
	}
	if facts.Spatial.HasSpatialIndex == nil || !*facts.Spatial.HasSpatialIndex {
		t.Fatalf("mixed-case spatial index facts = %#v", facts.Spatial)
	}
}

func readOracleIntegrationBatchAfterDDL(ctx context.Context, p *OraclePlugin, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		batch, err := p.ReadBatch(ctx, connInfo, path, opts)
		if err == nil {
			return batch, nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "ORA-01466") {
			return nil, err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

func oracleIntegrationMultiPolygon(t *testing.T, srid int, x, y float64) *geom.MultiPolygon {
	t.Helper()
	polygon := geom.NewPolygonFlat(geom.XY, []float64{
		x, y,
		x + 1000, y,
		x + 1000, y + 1000,
		x, y + 1000,
		x, y,
	}, []int{10}).SetSRID(srid)
	multi := geom.NewMultiPolygon(geom.XY).SetSRID(srid)
	if err := multi.Push(polygon); err != nil {
		t.Fatal(err)
	}
	return multi
}

package mysql

import (
	"context"
	"database/sql"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestMySQLSpatialFactsAndReadIntegration(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)

	qualified := mysqlDialect().QualifiedTable(database, "features")
	_, err := db.ExecContext(context.Background(), "CREATE TABLE "+qualified+" (id BIGINT PRIMARY KEY, updated_at DATETIME(6) NOT NULL, geom POINT SRID 4326 NOT NULL, parts GEOMETRYCOLLECTION SRID 4326 NULL, SPATIAL INDEX idx_features_geom (geom)) ENGINE=InnoDB")
	if err != nil {
		t.Fatalf("create spatial integration table: %v", err)
	}
	_, err = db.ExecContext(context.Background(), "INSERT INTO "+qualified+" (id, updated_at, geom, parts) VALUES (1, '2026-08-03 12:00:00.000000', ST_GeomFromText('POINT(121.5 31.2)', 4326, 'axis-order=long-lat'), ST_GeomFromText('GEOMETRYCOLLECTION(POINT(121.5 31.2))', 4326, 'axis-order=long-lat'))")
	if err != nil {
		t.Fatalf("insert spatial integration row: %v", err)
	}

	gormDB, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm integration connection: %v", err)
	}
	facts, err := mysqlPlugin.describeSpatialFacts(context.Background(), gormDB, database, "features", nil)
	if err != nil {
		t.Fatalf("describe MySQL spatial facts: %v", err)
	}
	if facts == nil || len(facts.GeometryColumns) != 2 || facts.GeometryColumns[0].GeometryType != "Point" || facts.GeometryColumns[1].GeometryType != "GeometryCollection" {
		t.Fatalf("spatial facts = %#v", facts)
	}
	if facts.GeometryColumns[0].SRID == nil || *facts.GeometryColumns[0].SRID != 4326 || facts.GeometryColumns[0].CRSRef != "EPSG:4326" {
		t.Fatalf("spatial CRS facts = %#v", facts.GeometryColumns[0])
	}
	if facts.HasSpatialIndex == nil || !*facts.HasSpatialIndex || facts.IndexName != "idx_features_geom" {
		t.Fatalf("spatial index facts = %#v", facts)
	}

	session, err := mysqlPlugin.OpenTableReadSession(context.Background(), connInfo, mysqlIntegrationTablePath(database, "features"), plugin.TableReadSessionOptions{Hints: map[string]interface{}{
		plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
	}})
	if err != nil {
		t.Fatalf("open MySQL spatial read session: %v", err)
	}
	defer session.Close(context.Background())
	batch, err := session.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("read MySQL spatial batch: %v", err)
	}
	if len(batch.Rows) != 1 || batch.Spatial == nil || len(batch.Spatial.GeometryColumns) != 2 {
		t.Fatalf("spatial batch = %#v", batch)
	}
	for _, column := range []string{"geom", "parts"} {
		encoded, ok := batch.Rows[0][column].([]byte)
		if !ok {
			t.Fatalf("%s value = %T, want []byte EWKB", column, batch.Rows[0][column])
		}
		geometry, err := ewkb.Unmarshal(encoded)
		if err != nil || geometry.SRID() != 4326 {
			t.Fatalf("decode %s EWKB: geometry=%#v err=%v", column, geometry, err)
		}
	}
	feature, err := mysqlPlugin.ReadSpatialFeature(context.Background(), connInfo, mysqlIntegrationTablePath(database, "features"), plugin.SpatialFeatureReadOptions{
		GeometryField: "geom",
		IdentityField: "id",
		IdentityValue: "1",
	})
	if err != nil {
		t.Fatalf("read MySQL spatial feature: %v", err)
	}
	if feature == nil || feature.SRID != 4326 || feature.Spatial == nil || feature.Spatial.PrimaryCRSRef() != "EPSG:4326" {
		t.Fatalf("MySQL spatial feature = %#v", feature)
	}
	featureGeometry, err := ewkb.Unmarshal(feature.GeometryEWKB)
	if err != nil {
		t.Fatalf("decode MySQL spatial feature geometry: %v", err)
	}
	point, ok := featureGeometry.(*geom.Point)
	if !ok || point.X() != 121.5 || point.Y() != 31.2 {
		t.Fatalf("MySQL spatial feature point = %#v", featureGeometry)
	}
	centroidGeometry, err := ewkb.Unmarshal(feature.CentroidEWKB)
	if err != nil {
		t.Fatalf("decode MySQL spatial feature centroid: %v", err)
	}
	centroid, ok := centroidGeometry.(*geom.Point)
	if !ok || centroid.X() != 121.5 || centroid.Y() != 31.2 {
		t.Fatalf("MySQL spatial feature centroid = %#v", centroidGeometry)
	}

	watermarkSession, err := mysqlPlugin.OpenBoundedWatermarkRead(context.Background(), connInfo, mysqlIntegrationTablePath(database, "features"), plugin.BoundedWatermarkReadOptions{
		WatermarkField: "updated_at",
		TieBreakers:    []string{"id"},
	})
	if err != nil {
		t.Fatalf("open MySQL spatial watermark session: %v", err)
	}
	defer watermarkSession.Close(context.Background())
	watermarkTable, watermarkSpatial := watermarkSession.TableInfo()
	if watermarkTable == nil || watermarkSpatial == nil || watermarkSpatial.PrimaryGeometryName() != "geom" {
		t.Fatalf("watermark table info = %#v spatial = %#v", watermarkTable, watermarkSpatial)
	}
	watermarkBatch, err := watermarkSession.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("read MySQL spatial watermark batch: %v", err)
	}
	if watermarkBatch.Spatial == nil || len(watermarkBatch.Rows) != 1 {
		t.Fatalf("watermark spatial batch = %#v", watermarkBatch)
	}
	for _, column := range []string{"geom", "parts"} {
		encoded, ok := watermarkBatch.Rows[0][column].([]byte)
		if !ok {
			t.Fatalf("watermark %s value = %T, want []byte EWKB", column, watermarkBatch.Rows[0][column])
		}
		geometry, err := ewkb.Unmarshal(encoded)
		if err != nil || geometry.SRID() != 4326 {
			t.Fatalf("decode watermark %s EWKB: geometry=%#v err=%v", column, geometry, err)
		}
	}

	hasSpatialIndex := true
	writeSpatial := datatype.NewSingleGeometrySpatialInfo("shape", "Polygon", 4326, 2)
	writeSpatial.HasSpatialIndex = &hasSpatialIndex
	writeSpatial.IndexName = "idx_written_shape"
	writeFields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "shape", Type: datatype.FieldTypeGeometry},
	}
	if err := mysqlPlugin.PrepareTableWrite(context.Background(), connInfo, mysqlIntegrationTablePath(database, "written_features"), plugin.TableWriteOptions{
		Fields: writeFields, SpatialInfo: writeSpatial,
	}); err != nil {
		t.Fatalf("prepare MySQL spatial table write: %v", err)
	}
	var dataType string
	var srsID sql.NullInt64
	if err := db.QueryRowContext(context.Background(), `
		SELECT data_type, srs_id FROM information_schema.columns
		WHERE table_schema = ? AND table_name = 'written_features' AND column_name = 'shape'
	`, database).Scan(&dataType, &srsID); err != nil {
		t.Fatalf("query prepared spatial column: %v", err)
	}
	if dataType != "polygon" || !srsID.Valid || srsID.Int64 != 4326 {
		t.Fatalf("prepared spatial column = type %q, SRID %#v", dataType, srsID)
	}
	var spatialIndexes int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = 'written_features' AND column_name = 'shape' AND index_type = 'SPATIAL' AND index_name = 'idx_written_shape'
	`, database).Scan(&spatialIndexes); err != nil {
		t.Fatalf("query prepared spatial index: %v", err)
	}
	if spatialIndexes != 1 {
		t.Fatalf("prepared spatial index count = %d", spatialIndexes)
	}
}

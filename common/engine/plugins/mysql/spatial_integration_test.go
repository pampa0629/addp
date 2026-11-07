package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkt"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestIntegrationMySQLGuardedSpatialTableRead(t *testing.T) {
	db, provider, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := context.Background()
	table := mysqlDialect().QualifiedTable(database, "locations")
	view := mysqlDialect().QualifiedTable(database, "same_shape_view")
	for _, statement := range []string{
		"CREATE TABLE " + table + " (id BIGINT PRIMARY KEY, position POINT SRID 3857 NOT NULL) ENGINE=InnoDB",
		"INSERT INTO " + table + " VALUES (1, ST_GeomFromText('POINT(12000 6000)', 3857))",
		"CREATE VIEW " + view + " AS SELECT id, position FROM " + table,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	path := plugin.TabularItemPath(17, "database", database, "locations")
	opts := plugin.TableReadSessionOptions{Hints: map[string]any{
		plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
	}}
	calls := 0
	var guarded plugin.GuardedTableReadSessionProvider = provider
	checkPath := func(_ context.Context, set *plugin.QueryReadSet, fields []datatype.FieldInfo) error {
		calls++
		if !reflect.DeepEqual(set.Paths, []plugin.EngineCatalogPath{path}) {
			t.Fatalf("read set does not prove the exact table: %#v", set)
		}
		if len(fields) != 2 || fields[0].Name != "id" || fields[1].Name != "position" || !datatype.IsSpatialFieldType(fields[1].Type) {
			t.Fatalf("actual guarded field schema: %#v", fields)
		}
		return nil
	}
	rejected := errors.New("source read denied")
	if session, err := guarded.OpenGuardedTableReadSession(ctx, connInfo, path, opts, func(ctx context.Context, set *plugin.QueryReadSet, fields []datatype.FieldInfo) error {
		_ = checkPath(ctx, set, fields)
		return rejected
	}); session != nil || !errors.Is(err, rejected) || calls != 1 {
		t.Fatalf("denied source returned a session or skipped authorization: %v, %v, %d", session, err, calls)
	}
	viewPath := plugin.TabularItemPath(17, "database", database, "same_shape_view")
	if session, err := guarded.OpenGuardedTableReadSession(ctx, connInfo, viewPath, opts, checkPath); session != nil ||
		!errors.Is(err, plugin.ErrQueryReadSetUnresolved) || calls != 1 {
		t.Fatalf("view reached the authorization callback or opened a session: %v, %v, %d", session, err, calls)
	}
	wrongRoot := plugin.TabularItemPath(17, "database", database, "locations")
	wrongRoot.Segments[0] = plugin.EngineCatalogSegment{Term: plugin.EngineCatalogTermService, Kind: plugin.EngineCatalogTermService}
	if session, err := guarded.OpenGuardedTableReadSession(ctx, connInfo, wrongRoot, opts, checkPath); session != nil ||
		!errors.Is(err, plugin.ErrQueryReadSetUnresolved) || calls != 1 {
		t.Fatalf("malformed source identity reached the callback: %v, %v, %d", session, err, calls)
	}
	session, err := guarded.OpenGuardedTableReadSession(ctx, connInfo, path, opts, checkPath)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)
	if calls != 2 {
		t.Fatalf("allowed source must be checked exactly once, calls=%d", calls)
	}
	set, err := session.(plugin.ReadSessionEvidence).ReadSet(ctx)
	if err != nil || !reflect.DeepEqual(set.Paths, []plugin.EngineCatalogPath{path}) {
		t.Fatalf("session read evidence = %#v, %v", set, err)
	}
	batch, err := session.ReadBatch(ctx, 10)
	if err != nil || len(batch.Rows) != 1 || batch.Spatial.PrimarySRIDValue() != 3857 {
		t.Fatalf("guarded spatial batch = %#v, %v", batch, err)
	}
	geometry, err := ewkb.Unmarshal(batch.Rows[0]["position"].([]byte))
	point, ok := geometry.(*geom.Point)
	if err != nil || !ok || point.SRID() != 3857 || point.X() != 12000 || point.Y() != 6000 {
		t.Fatalf("guarded native geometry = %#v, %v", geometry, err)
	}
}

func TestIntegrationMySQLFileGDBSourceShapes(t *testing.T) {
	db, provider, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	for _, tc := range []struct {
		name, kind, text string
		srid             int
	}{
		{"points", "POINT", "POINT (12000 6000)", 3857},
		{"lines", "MULTILINESTRING", "MULTILINESTRING ((110 28,111 29,112 29.25),(114 30,115 30.5))", 4326},
		{"polygons", "MULTIPOLYGON", "MULTIPOLYGON (((110 28,114 28,114 32,110 32,110 28),(111 29,111 30,112 30,112 29,111 29)),((115 28,117 28,117 29,115 28)))", 4326},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			qualified := mysqlDialect().QualifiedTable(database, tc.name)
			if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (id INTEGER PRIMARY KEY, shape %s SRID %d NULL) ENGINE=InnoDB", qualified, tc.kind, tc.srid)); err != nil {
				t.Fatal(err)
			}
			constructor := "ST_GeomFromText(?, ?)"
			if tc.srid == 4326 {
				constructor = "ST_GeomFromText(?, ?, 'axis-order=long-lat')"
			}
			if _, err := db.ExecContext(ctx, "INSERT INTO "+qualified+" VALUES (1,"+constructor+"),(2,NULL)", tc.text, tc.srid); err != nil {
				t.Fatal(err)
			}
			path := plugin.TabularItemPath(17, "database", database, tc.name)
			checked := false
			session, err := provider.OpenGuardedTableReadSession(ctx, connInfo, path, plugin.TableReadSessionOptions{Hints: map[string]any{
				plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
			}}, func(_ context.Context, set *plugin.QueryReadSet, _ []datatype.FieldInfo) error {
				checked = true
				if !reflect.DeepEqual(set.Paths, []plugin.EngineCatalogPath{path}) {
					t.Fatal("FileGDB source shape has an imprecise read set")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(ctx)
			batch, err := session.ReadBatch(ctx, 10)
			if err != nil || !checked || batch == nil || len(batch.Rows) != 2 || batch.Spatial.PrimarySRIDValue() != tc.srid {
				t.Fatalf("FileGDB source batch: %v", err)
			}
			want, err := wkt.Unmarshal(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			canonical, _ := wkt.Marshal(want)
			nulls, shapes := 0, 0
			for _, row := range batch.Rows {
				if row["shape"] == nil {
					nulls++
					continue
				}
				encoded, ok := row["shape"].([]byte)
				if !ok {
					t.Fatal("FileGDB source geometry is not EWKB")
				}
				value, err := ewkb.Unmarshal(encoded)
				if err != nil || value.SRID() != tc.srid || value.Layout() != geom.XY {
					t.Fatalf("FileGDB source CRS or dimension: %v", err)
				}
				got, _ := wkt.Marshal(value)
				if got != canonical {
					t.Fatalf("FileGDB source coordinates or parts differ: %s", got)
				}
				shapes++
			}
			if nulls != 1 || shapes != 1 {
				t.Fatal("FileGDB source null geometry differs")
			}
		})
	}
}

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

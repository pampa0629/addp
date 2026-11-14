package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	mysqldriver "github.com/go-sql-driver/mysql"
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
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Run("bound page and shared pool", func(t *testing.T) { verifyMySQLGuardedPage(t, db, provider, connInfo, database) })
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

func verifyMySQLGuardedPage(t *testing.T, db *sql.DB, p *MySQLPlugin, info plugin.ConnectionInfo, database string) {
	t.Helper()
	ctx := plugin.WithPoolEngineVersion(t.Context(), 987641, 1)
	info = maps.Clone(info)
	info["pool_max_open_conns"], info["pool_max_idle_conns"] = 1, 1
	t.Cleanup(func() { _ = plugin.ClosePool(987641) })
	table := mysqlDialect().QualifiedTable(database, "guarded_page")
	for _, q := range []string{
		"CREATE TABLE " + table + " (id BIGINT PRIMARY KEY, value DECIMAL(38,18), label VARCHAR(40)) ENGINE=InnoDB",
		"INSERT INTO " + table + " VALUES (9007199254740993,12345678901234567890.123456789012345678,'kept '),(9007199254740994,NULL,'kept '),(9007199254740995,1,'other')",
		"CREATE VIEW " + mysqlDialect().QualifiedTable(database, "page_view") + " AS SELECT * FROM " + table,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	path := plugin.TabularItemPath(987641, "database", database, "guarded_page")
	opts := plugin.TableReadSessionOptions{Page: &plugin.CatalogTablePageOptions{Limit: 1, Offset: 1, Predicate: "`label` = ?", Args: []any{"kept "}}}
	checks := 0
	check := func(_ context.Context, set *plugin.QueryReadSet, fields []datatype.FieldInfo) error {
		checks++
		if !reflect.DeepEqual(set.Paths, []plugin.EngineCatalogPath{path}) || len(fields) != 3 || fields[1].Precision != 38 || fields[1].Scale != 18 {
			t.Fatalf("actual page identity/fields: %#v %#v", set, fields)
		}
		return nil
	}
	denied := errors.New("current grant revoked")
	if s, e := p.OpenGuardedTableReadSession(ctx, info, path, opts, func(c context.Context, r *plugin.QueryReadSet, f []datatype.FieldInfo) error {
		_ = check(c, r, f)
		return denied
	}); s != nil || !errors.Is(e, denied) {
		t.Fatalf("denied page opened: %v", e)
	}
	s, err := p.OpenGuardedTableReadSession(ctx, info, path, opts, check)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	batch, err := s.ReadBatch(ctx, 10)
	if err != nil || len(batch.Rows) != 1 || batch.Offset != 1 || fmt.Sprint(batch.Rows[0]["id"]) != "9007199254740994" || batch.Rows[0]["value"] != nil || batch.Rows[0]["label"] != "kept " {
		t.Fatalf("bounded typed page: %#v %v", batch, err)
	}
	proof, err := s.(plugin.ReadSessionEvidence).ReadSet(ctx)
	if err != nil || !reflect.DeepEqual(proof.Paths, []plugin.EngineCatalogPath{path}) {
		t.Fatalf("session proof: %#v %v", proof, err)
	}
	// A concurrent shape change cannot pass the same session's metadata lock.
	ddlConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ddlConn.Close()
	if _, err := ddlConn.ExecContext(ctx, "SET SESSION lock_wait_timeout=1"); err != nil {
		t.Fatal(err)
	}
	ddlCtx, stop := context.WithTimeout(t.Context(), 3*time.Second)
	_, ddlErr := ddlConn.ExecContext(ddlCtx, "ALTER TABLE "+table+" ADD COLUMN forbidden INT")
	stop()
	// Wait for the server's statement failure. A cancelled client alone does
	// not prove that the server discarded its queued DDL before source unlock.
	if ddlErr == nil || !strings.Contains(ddlErr.Error(), "1205") {
		t.Fatalf("DDL did not fail on the held metadata lock: %v", ddlErr)
	}
	if _, err := ddlConn.ExecContext(ctx, "SET SESSION lock_wait_timeout=DEFAULT"); err != nil {
		t.Fatal(err)
	}
	if err := ddlConn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	opts.Page.Offset = 0
	s, err = p.OpenGuardedTableReadSession(ctx, info, path, opts, check)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	batch, err = s.ReadBatch(ctx, 1)
	if err != nil || len(batch.Rows) != 1 || fmt.Sprint(batch.Rows[0]["id"]) != "9007199254740993" || fmt.Sprint(batch.Rows[0]["value"]) != "12345678901234567890.123456789012345678" {
		t.Fatalf("recovered exact first page: %#v %v", batch, err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{ID: path.EngineID, EngineType: p.Type(), Version: 1, ConnectionInfo: info}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pooledDB, err := pool.DB()
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := pooledDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	wait, stop := context.WithTimeout(t.Context(), 150*time.Millisecond)
	wait = plugin.WithPoolEngineVersion(wait, path.EngineID, 1)
	_, waitErr := p.OpenGuardedTableReadSession(wait, info, path, opts, check)
	stop()
	if !errors.Is(waitErr, context.DeadlineExceeded) || plugin.QueryErrorCodeOf(waitErr) != plugin.QueryErrorCodeConnectionAcquisitionTimeout {
		t.Fatalf("guarded page bypassed shared pool wait: %v", waitErr)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	if pooledDB.Stats().InUse != 0 || pooledDB.PingContext(ctx) != nil {
		t.Fatal("guarded session closed or leaked shared pool")
	}
	view := plugin.TabularItemPath(path.EngineID, "database", database, "page_view")
	before := checks
	if s, e := p.OpenGuardedTableReadSession(ctx, info, view, opts, check); s != nil || !errors.Is(e, plugin.ErrQueryReadSetUnresolved) || checks != before {
		t.Fatalf("indirect view reached content authorization: %v", e)
	}
	// The source budget can fail while binding the locked table, before the
	// client deadline and before the owner authorization callback is reached.
	budgetConn, err := pooledDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var originalBudget uint64
	if err := budgetConn.QueryRowContext(ctx, "SELECT @@session.max_execution_time").Scan(&originalBudget); err != nil {
		budgetConn.Close()
		t.Fatal(err)
	}
	if _, err := budgetConn.ExecContext(ctx, "SET SESSION max_execution_time=100"); err != nil {
		budgetConn.Close()
		t.Fatal(err)
	}
	budgetConn.Close()
	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if _, err := holder.ExecContext(ctx, "LOCK TABLES "+table+" WRITE"); err != nil {
		t.Fatal(err)
	}
	defer holder.ExecContext(context.Background(), "UNLOCK TABLES")
	bounded, stop := context.WithTimeout(ctx, 3*time.Second)
	before = checks
	blocked, sourceErr := p.OpenGuardedTableReadSession(bounded, info, path, opts, check)
	var native *mysqldriver.MySQLError
	if blocked != nil || !errors.As(sourceErr, &native) || native.Number != 3024 || plugin.QueryErrorCodeOf(sourceErr) != plugin.QueryErrorCodeSQLExecutionTimeout || !errors.Is(sourceErr, context.DeadlineExceeded) || bounded.Err() != nil || checks != before {
		stop()
		t.Fatalf("guarded source timeout lost native 3024 or execution classification: %v code=%s", sourceErr, plugin.QueryErrorCodeOf(sourceErr))
	}
	stop()
	if _, err := holder.ExecContext(ctx, "UNLOCK TABLES"); err != nil {
		t.Fatal(err)
	}
	holder.Close()
	budgetConn, err = pooledDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var restored uint64
	if err := budgetConn.QueryRowContext(ctx, "SELECT @@session.max_execution_time").Scan(&restored); err != nil {
		budgetConn.Close()
		t.Fatal(err)
	}
	if restored != 100 {
		budgetConn.Close()
		t.Fatalf("source budget was not restored: %d", restored)
	}
	if _, err := budgetConn.ExecContext(ctx, fmt.Sprintf("SET SESSION max_execution_time=%d", originalBudget)); err != nil {
		budgetConn.Close()
		t.Fatal(err)
	}
	budgetConn.Close()
	s, err = p.OpenGuardedTableReadSession(ctx, info, path, opts, check)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	if batch, err := s.ReadBatch(ctx, 1); err != nil || len(batch.Rows) != 1 {
		t.Fatalf("guarded page did not recover: %#v %v", batch, err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	poisoned, err := pooledDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := poisoned.ExecContext(ctx, "CREATE TEMPORARY TABLE "+table+" (id BIGINT PRIMARY KEY, value DECIMAL(38,18), label VARCHAR(40))"); err != nil {
		poisoned.Close()
		t.Fatal(err)
	}
	if err := poisoned.Close(); err != nil {
		t.Fatal(err)
	}
	before = checks
	if s, e := p.OpenGuardedTableReadSession(ctx, info, path, opts, check); s != nil || !errors.Is(e, plugin.ErrQueryReadSetUnresolved) || checks != before {
		t.Fatalf("temporary shadow was authorized as the persistent table: %v", e)
	}
}

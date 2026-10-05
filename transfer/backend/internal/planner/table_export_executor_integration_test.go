package planner

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/format"
	geojsonformat "github.com/addp/common/format/plugins/geojson"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/testpg"
	_ "github.com/lib/pq"
)

func TestIntegrationPlannerPlanExecutesPostgresGeoJSONReadTransform(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL/PostGIS integration test")
	}

	ctx := context.Background()
	connInfo := plannerIntegrationPostgresConnInfo(t)
	pg := &postgresql.PostgreSQLPlugin{}
	db := openPlannerIntegrationPostgres(t, ctx, pg, connInfo)
	if _, err := db.ExecContext(ctx, "SELECT postgis_version()"); err != nil {
		t.Fatalf("PostGIS is required for GeoJSON integration test: %v", err)
	}

	schemaName := plannerIntegrationPostgresTestSchema(t, ctx, db)
	tableName := fmt.Sprintf("planner_pg_3857_to_geojson_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE "%s"."%s" (
			id integer PRIMARY KEY,
			name text,
			geometry geometry(Point,3857)
		)
	`, schemaName, tableName)); err != nil {
		t.Fatalf("create source table failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO "%s"."%s" (id, name, geometry)
		VALUES (1, 'Planner Mercator point', ST_Transform(ST_SetSRID(ST_MakePoint(10, 0), 4326), 3857))
	`, schemaName, tableName)); err != nil {
		t.Fatalf("insert source rows failed: %v", err)
	}

	spec := minimalNativeToEncodedSpec()
	spec.Source.Locator = tableLocator(1, schemaName, tableName)
	spec.Source.Attributes = tableSourceAttributes("single", "table", schemaName+"/"+tableName, nil, []map[string]interface{}{
		{"name": "id", "type": "int"},
		{"name": "name", "type": "string"},
		{"name": "geometry", "type": "geometry"},
	}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("geometry", "Point", 3857, 2)))
	spec.Target.Format = format.FormatGeoJSON
	spec.Target.Options = map[string]interface{}{"geometry_field": "geometry"}
	setFileTarget(&spec, 2, "exports/"+tableName+".geojson")

	caps := pg.Capabilities()
	build, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql", ConnInfo: connInfo, Capabilities: &caps},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if len(build.Plan.Transforms) != 0 {
		t.Fatalf("planner transforms = %#v, want PG source-native read transform", build.Plan.Transforms)
	}
	if got := build.Plan.Source.ReadOptions[engineplugin.TableReadHintGeometryEncoding]; got != string(format.GeometryEncodingGeoJSON) {
		t.Fatalf("planner read options = %#v, want geometry_encoding=geojson", build.Plan.Source.ReadOptions)
	}

	target := &plannerIntegrationContentWriter{}
	tableExecutor := &executor.TableTransferExecutor{
		SourceNativeReader:         pg,
		SourceTableSessionProvider: pg,
		TargetContentWriter:        target,
		TargetTableWriterProvider:  geojsonformat.NewPlugin(nil),
		TargetDeleteProvider:       target,
	}
	metrics, err := tableExecutor.Execute(ctx, build.Plan)
	if err != nil {
		t.Fatalf("Execute planned transfer failed: %v", err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 {
		t.Fatalf("metrics = %#v, want one transferred row", metrics)
	}

	feature := executorFirstGeoJSONFeature(t, target.buf.Bytes())
	geometry, ok := feature["geometry"].(map[string]interface{})
	if !ok {
		t.Fatalf("geometry = %#v, want GeoJSON geometry object", feature["geometry"])
	}
	coords, ok := geometry["coordinates"].([]interface{})
	if !ok || len(coords) != 2 {
		t.Fatalf("coordinates = %#v, want point coordinate pair", geometry["coordinates"])
	}
	x, okX := coords[0].(float64)
	y, okY := coords[1].(float64)
	if !okX || !okY || math.Abs(x-10) > 1e-9 || math.Abs(y) > 1e-9 {
		t.Fatalf("GeoJSON coordinates = %#v, want approximately [10, 0]", coords)
	}
}

func TestIntegrationPlannerTargetOverrideAppendsOnlyToExistingPostgresTable(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	// Keep non-spatial coverage independent of extensions installed in public.
	// lib/pq applies this to fixture and Provider connections; t.Setenv restores it.
	t.Setenv("PGOPTIONS", "-c search_path=pg_catalog")

	ctx := context.Background()
	connInfo := plannerIntegrationPostgresConnInfo(t)
	pg := &postgresql.PostgreSQLPlugin{}
	db := openPlannerIntegrationPostgres(t, ctx, pg, connInfo)
	var postgisUnavailable bool
	if err := db.QueryRowContext(ctx, "SELECT to_regprocedure('postgis_version()') IS NULL").Scan(&postgisUnavailable); err != nil {
		t.Fatalf("check non-spatial test isolation failed: %v", err)
	}
	if !postgisUnavailable {
		t.Fatal("target override test must run without visible PostGIS functions")
	}

	schemaName := plannerIntegrationPostgresTestSchema(t, ctx, db)
	sourceTable := "override_source"
	defaultTarget := "default_target"
	overrideTarget := "override_target"
	for _, tableName := range []string{sourceTable, defaultTarget, overrideTarget} {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`
			CREATE TABLE "%s"."%s" (
				id bigint NOT NULL,
				name text NOT NULL
			)
		`, schemaName, tableName)); err != nil {
			t.Fatalf("create table %s failed: %v", tableName, err)
		}
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO "%s"."%s" (id, name)
		VALUES (1, 'first'), (2, 'second')
	`, schemaName, sourceTable)); err != nil {
		t.Fatalf("insert source rows failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO "%s"."%s" (id, name) VALUES (900, 'default sentinel')
	`, schemaName, defaultTarget)); err != nil {
		t.Fatalf("insert default target sentinel failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO "%s"."%s" (id, name) VALUES (800, 'override sentinel')
	`, schemaName, overrideTarget)); err != nil {
		t.Fatalf("insert override target sentinel failed: %v", err)
	}

	spec := overridableNativeTableSpec()
	spec.Source.Locator = tableLocator(1, schemaName, sourceTable)
	spec.Target.ParentLocator = schemaLocator(2, schemaName)
	spec.Target.Name = defaultTarget
	spec.Transforms[0].Fields = []FieldMappingSpec{
		{Source: "id", Target: "id", TargetType: "bigint"},
		{Source: "name", Target: "name", TargetType: "string"},
	}
	resolved, err := ResolveTargetOverride(spec, tableLocator(2, schemaName, overrideTarget))
	if err != nil {
		t.Fatalf("ResolveTargetOverride failed: %v", err)
	}

	caps := pg.Capabilities()
	build, err := BuildTableTransferPlan(resolved, StaticEngineResolver{
		1: {Type: "postgresql", ConnInfo: connInfo, Capabilities: &caps},
		2: {Type: "postgresql", ConnInfo: connInfo, Capabilities: &caps},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if !build.Plan.Target.ManagedExisting || build.Plan.Target.DeleteBeforeWrite {
		t.Fatalf("override target plan = %#v, want managed existing append", build.Plan.Target)
	}

	tableExecutor := &executor.TableTransferExecutor{
		SourceNativeReader:         pg,
		SourceTableSessionProvider: pg,
		TargetTableSessionProvider: pg,
	}
	metrics, err := tableExecutor.Execute(ctx, build.Plan)
	if err != nil {
		t.Fatalf("Execute target override transfer failed: %v", err)
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 {
		t.Fatalf("metrics = %#v, want two transferred rows", metrics)
	}

	assertPlannerIntegrationTableIDs(t, ctx, db, schemaName, defaultTarget, "900")
	assertPlannerIntegrationTableIDs(t, ctx, db, schemaName, overrideTarget, "1,2,800")
}

func assertPlannerIntegrationTableIDs(t *testing.T, ctx context.Context, db *sql.DB, schemaName, tableName, want string) {
	t.Helper()
	var got string
	query := fmt.Sprintf(`SELECT string_agg(id::text, ',' ORDER BY id) FROM "%s"."%s"`, schemaName, tableName)
	if err := db.QueryRowContext(ctx, query).Scan(&got); err != nil {
		t.Fatalf("read table %s ids failed: %v", tableName, err)
	}
	if got != want {
		t.Fatalf("table %s ids = %q, want %q", tableName, got, want)
	}
}

func plannerIntegrationPostgresConnInfo(t *testing.T) engineplugin.ConnectionInfo {
	t.Helper()
	return testpg.ConnInfoFromEnv(t)
}

func openPlannerIntegrationPostgres(t *testing.T, ctx context.Context, pg *postgresql.PostgreSQLPlugin, connInfo engineplugin.ConnectionInfo) *sql.DB {
	t.Helper()

	connStr, err := pg.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("open postgres failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("connect PostgreSQL failed: %v", err)
	}
	testpg.DropSchemasWithPrefixes(t, ctx, db, "transfer_planner_test_")
	return db
}

func plannerIntegrationPostgresTestSchema(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()

	schemaName := fmt.Sprintf("transfer_planner_test_%d", time.Now().UnixNano())
	testpg.CreateSchema(t, ctx, db, schemaName)
	return schemaName
}

func executorFirstGeoJSONFeature(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var collection map[string]interface{}
	if err := json.Unmarshal(data, &collection); err != nil {
		t.Fatalf("unmarshal GeoJSON failed: %v; output=%s", err, string(data))
	}
	features, ok := collection["features"].([]interface{})
	if !ok || len(features) != 1 {
		t.Fatalf("features = %#v, want one feature", collection["features"])
	}
	feature, ok := features[0].(map[string]interface{})
	if !ok {
		t.Fatalf("feature = %#v, want object", features[0])
	}
	return feature
}

type plannerIntegrationContentWriter struct {
	buf bytes.Buffer
}

func (w *plannerIntegrationContentWriter) Type() string { return "planner_integration_writer" }

func (w *plannerIntegrationContentWriter) DisplayName() string { return "Planner Integration Writer" }

func (w *plannerIntegrationContentWriter) EngineOrigin() string { return "general" }

func (w *plannerIntegrationContentWriter) DefaultPort() int { return 0 }

func (w *plannerIntegrationContentWriter) RequiredFields() []string { return nil }

func (w *plannerIntegrationContentWriter) SensitiveFields() []string { return nil }

func (w *plannerIntegrationContentWriter) ValidateConnectionInfo(engineplugin.ConnectionInfo) error {
	return nil
}

func (w *plannerIntegrationContentWriter) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (w *plannerIntegrationContentWriter) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (w *plannerIntegrationContentWriter) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (w *plannerIntegrationContentWriter) CreateContent(context.Context, engineplugin.ConnectionInfo, engineplugin.EngineCatalogPath, engineplugin.WriteOptions) (io.WriteCloser, error) {
	w.buf.Reset()
	return plannerIntegrationWriteCloser{Writer: &w.buf}, nil
}

func (w *plannerIntegrationContentWriter) DeleteResource(context.Context, engineplugin.ConnectionInfo, engineplugin.EngineCatalogPath) error {
	return nil
}

type plannerIntegrationWriteCloser struct {
	io.Writer
}

func (w plannerIntegrationWriteCloser) Close() error {
	return nil
}

func TestIntegrationPlannerMultiSourceQueryFieldLineage(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	t.Setenv("PGOPTIONS", "-c search_path=pg_catalog")
	ctx := t.Context()
	conn := plannerIntegrationPostgresConnInfo(t)
	pg := &postgresql.PostgreSQLPlugin{}
	db := openPlannerIntegrationPostgres(t, ctx, pg, conn)
	schema := plannerIntegrationPostgresTestSchema(t, ctx, db)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE "%[1]s".persons (id integer, name text);
		CREATE TABLE "%[1]s".activities (id integer, name text);
		CREATE TABLE "%[1]s".allowed (id integer);
		INSERT INTO "%[1]s".persons VALUES (1, 'Alice'), (2, 'Bob');
		INSERT INTO "%[1]s".activities VALUES (1, 'Hiking'), (2, 'Running');
		INSERT INTO "%[1]s".allowed VALUES (1);
	`, schema)); err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf(`WITH joined AS (
		SELECT p.id, p.name || ':' || a.name AS label
		FROM "%[1]s".persons p JOIN "%[1]s".activities a ON p.id=a.id
		WHERE EXISTS (SELECT 1 FROM "%[1]s".allowed s WHERE s.id=p.id)
	) SELECT id, label FROM joined UNION ALL SELECT id, label FROM joined`, schema)
	fields := []map[string]interface{}{
		{"source": "id", "target": "person_id", "target_type": "bigint"},
		{"source": "label", "target": "person_label", "target_type": "string"},
	}
	spec, err := ParseTableExportTaskSpec(map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"}, "load": map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{"locator": tableLocator(1, schema, "persons"), "data_type": "table", "representation": "native", "query": map[string]interface{}{
			"language": "sql", "statement": query, "inputs": []map[string]interface{}{
				{"name": "events", "locator": tableLocator(1, schema, "activities")},
				{"name": "people", "locator": tableLocator(1, schema, "persons")},
				{"name": "scope", "locator": tableLocator(1, schema, "allowed")},
			},
		}},
		"target":     map[string]interface{}{"parent_locator": fmt.Sprintf("addp://engine/1/path/%s?type=schema", schema), "name": "query_export", "data_type": "table", "representation": "native", "policy": map[string]interface{}{"apply_mode": "replace"}},
		"transforms": []map[string]interface{}{{"type": "field_mapping", "version": "v1", "mode": "project", "fields": fields}},
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	caps := pg.Capabilities()
	build, err := BuildTableTransferPlan(spec, StaticEngineResolver{1: {Type: "postgresql", ConnInfo: conn, Capabilities: &caps}})
	if err != nil {
		t.Fatal(err)
	}
	tableExecutor, err := executor.NewTableTransferExecutor("postgresql", "postgresql", "", "")
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := tableExecutor.Execute(ctx, build.Plan)
	if err != nil {
		t.Fatal(err)
	}
	lineage := metrics.FieldLineage
	if metrics.RecordsWritten != 2 || lineage == nil || len(lineage.Sources) != 3 || len(lineage.Mappings) != 3 {
		t.Fatalf("multi-source field lineage missing: %+v", metrics)
	}
	for port, snapshot := range lineage.Sources {
		if snapshot.Validate() != nil || snapshot.HasField("label") {
			t.Fatalf("query alias replaced %s source schema: %+v", port, snapshot)
		}
	}
	want := map[string]string{"people/id/person_id": "derived", "people/name/person_label": "derived", "events/name/person_label": "derived"}
	for _, mapping := range lineage.Mappings {
		key := mapping.InputPort + "/" + mapping.SourceField + "/" + mapping.TargetField
		if want[key] != mapping.Transformation {
			t.Fatalf("unexpected mapping: %+v", mapping)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing mappings: %+v", want)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT person_id, person_label FROM "%s".query_export`, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id int64
		var label string
		if err := rows.Scan(&id, &label); err != nil {
			t.Fatal(err)
		}
		if id != 1 || label != "Alice:Hiking" {
			t.Fatalf("unexpected physical row: %d %s", id, label)
		}
		count++
	}
	if rows.Err() != nil || count != 2 {
		t.Fatalf("physical row count=%d, err=%v", count, rows.Err())
	}
}

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
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/format"
	geojsonformat "github.com/addp/common/format/plugins/geojson"
	"github.com/addp/transfer/internal/executor"
	_ "github.com/lib/pq"
)

func TestIntegrationPlannerPlanExecutesPostgresGeoJSONReadTransform(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL/PostGIS integration test")
	}

	ctx := context.Background()
	connInfo := plannerIntegrationPostgresConnInfo()
	pg := &postgresql.PostgreSQLPlugin{}
	db := openPlannerIntegrationPostgres(t, ctx, pg, connInfo)
	defer db.Close()

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

func plannerIntegrationPostgresConnInfo() engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"host":     plannerIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		"port":     plannerIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		"user":     plannerIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		"password": plannerIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		"database": plannerIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp"),
		"sslmode":  plannerIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	}
}

func plannerIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
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
	if _, err := db.ExecContext(ctx, "SELECT postgis_version()"); err != nil {
		_ = db.Close()
		t.Skipf("PostGIS is not available: %v", err)
	}
	return db
}

func plannerIntegrationPostgresTestSchema(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()

	schemaName := fmt.Sprintf("transfer_planner_test_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)); err != nil {
		t.Fatalf("create test schema failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName))
	})
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

func (w *plannerIntegrationContentWriter) CreateContent(context.Context, engineplugin.ConnectionInfo, engineplugin.CatalogPath, engineplugin.WriteOptions) (io.WriteCloser, error) {
	w.buf.Reset()
	return plannerIntegrationWriteCloser{Writer: &w.buf}, nil
}

func (w *plannerIntegrationContentWriter) DeleteResource(context.Context, engineplugin.ConnectionInfo, engineplugin.CatalogPath) error {
	return nil
}

type plannerIntegrationWriteCloser struct {
	io.Writer
}

func (w plannerIntegrationWriteCloser) Close() error {
	return nil
}

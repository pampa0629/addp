package service

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
	parquetformat "github.com/addp/common/format/plugins/parquet"
	commonModels "github.com/addp/common/models"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/testpg"
	_ "github.com/lib/pq"
	"github.com/twpayne/go-geom"
)

func TestIntegrationPostGIS3857ToGeoParquetUsesRuntimePROJJSON(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL/PostGIS integration test")
	}
	runtimeURL := strings.TrimSpace(os.Getenv("ADDP_GEOPYTHON_INTEGRATION_URL"))
	if runtimeURL == "" {
		t.Skip("set ADDP_GEOPYTHON_INTEGRATION_URL to run GeoPython Workflow integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	connInfo := testpg.ConnInfoFromEnv(t)
	pg := &postgresql.PostgreSQLPlugin{}
	db := openCRSIntegrationPostgres(t, ctx, pg, connInfo)

	schemaName := fmt.Sprintf("transfer_crs_test_%d", time.Now().UnixNano())
	testpg.CreateSchema(t, ctx, db, schemaName)
	tableName := "mercator_points"
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE "%s"."%s" (
			id integer PRIMARY KEY,
			name text,
			shape geometry(Point,3857)
		)
	`, schemaName, tableName)); err != nil {
		t.Fatalf("create source table: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO "%s"."%s" (id, name, shape)
		VALUES
			(1, 'Alpha', ST_Transform(ST_SetSRID(ST_MakePoint(120, 30), 4326), 3857)),
			(2, 'Beta', ST_Transform(ST_SetSRID(ST_MakePoint(121, 31), 4326), 3857))
	`, schemaName, tableName)); err != nil {
		t.Fatalf("insert source rows: %v", err)
	}

	sourcePath := engineplugin.TabularItemPath(0, engineplugin.CatalogTermSchema, schemaName, tableName)
	facts, err := pg.DescribeCatalogFacts(ctx, connInfo, sourcePath, engineplugin.CatalogFactsOptions{IncludeSpatialFacts: true})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts: %v", err)
	}
	if facts.Table == nil || facts.Spatial == nil {
		t.Fatalf("source facts = %#v, want table and spatial facts", facts)
	}
	sourceDefinition := facts.Spatial.CRSDefinitionByID("EPSG:3857")
	if sourceDefinition == nil || sourceDefinition.DefinitionEncoding != datatype.CRSDefinitionEncodingWKT || strings.TrimSpace(sourceDefinition.Definition) == "" {
		t.Fatalf("source CRS definition = %#v, want PostGIS EPSG:3857 WKT", sourceDefinition)
	}

	runtimeEngine := workflowReprojectTestEngine(t, runtimeURL, "geopython_workflow")
	converter := newWorkflowCRSDefinitionConverter(func(context.Context) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
		return runtimeEngine, commonModels.OperatorDescriptor{Name: "crs_to_projjson"}, nil
	})
	target := &crsIntegrationContentWriter{}
	parquetPlugin := parquetformat.NewPlugin()
	tableExecutor := &executor.TableTransferExecutor{
		SourceNativeReader:         pg,
		SourceTableSessionProvider: pg,
		TargetContentWriter:        target,
		TargetTableWriterProvider:  parquetPlugin,
		TargetCRSRequirements:      parquetPlugin,
		CRSDefinitionConverter:     converter,
	}
	metrics, err := tableExecutor.Execute(ctx, executor.TableTransferPlan{
		Source: executor.TableSourcePlan{
			Kind:        executor.TableEndpointNative,
			ConnInfo:    connInfo,
			Path:        sourcePath,
			TableInfo:   facts.Table,
			SpatialInfo: facts.Spatial,
			ReadOptions: map[string]interface{}{
				engineplugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
			},
		},
		Target: executor.TableTargetPlan{
			Kind:   executor.TableEndpointEncoded,
			Path:   engineplugin.FileItemPath(0, "exports/mercator_points.parquet"),
			Format: format.FormatParquet,
		},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 || metrics.Batches != 2 {
		t.Fatalf("metrics = %#v, want two rows in two batches", metrics)
	}
	if sourceDefinition.DefinitionEncoding != datatype.CRSDefinitionEncodingWKT {
		t.Fatalf("source spatial facts were mutated: %#v", sourceDefinition)
	}

	described, err := parquetPlugin.DescribeTable(ctx, bytes.NewReader(target.Bytes()), nil)
	if err != nil {
		t.Fatalf("DescribeTable GeoParquet: %v", err)
	}
	if described.Spatial == nil || described.Spatial.CRSRef != "EPSG:3857" || described.Spatial.PrimarySRIDValue() != 3857 {
		t.Fatalf("GeoParquet spatial = %#v, want EPSG:3857", described.Spatial)
	}
	projJSONDefinition := described.Spatial.CRSDefinitionByID("EPSG:3857")
	if projJSONDefinition == nil || projJSONDefinition.DefinitionEncoding != datatype.CRSDefinitionEncodingPROJJSON {
		t.Fatalf("GeoParquet CRS definition = %#v, want EPSG:3857 PROJJSON", projJSONDefinition)
	}
	var projJSON map[string]interface{}
	if err := json.Unmarshal([]byte(projJSONDefinition.Definition), &projJSON); err != nil {
		t.Fatalf("decode GeoParquet PROJJSON: %v", err)
	}
	id, _ := projJSON["id"].(map[string]interface{})
	if projJSON["type"] != "ProjectedCRS" || id["authority"] != "EPSG" || id["code"] != float64(3857) {
		t.Fatalf("GeoParquet PROJJSON identity = %#v, want EPSG:3857 ProjectedCRS", projJSON)
	}

	rows, err := parquetPlugin.SampleTable(ctx, bytes.NewReader(target.Bytes()), 0, 2, nil)
	if err != nil {
		t.Fatalf("SampleTable GeoParquet: %v", err)
	}
	wantCoordinates := [][2]float64{{13358338.895192828, 3503549.8435043753}, {13469658.385986103, 3632749.143384426}}
	if len(rows) != len(wantCoordinates) {
		t.Fatalf("GeoParquet rows = %#v, want two rows", rows)
	}
	for index, want := range wantCoordinates {
		geometry, err := commonSpatial.DecodeGeometryValue(rows[index]["shape"], string(format.GeometryEncodingWKB), 0)
		if err != nil {
			t.Fatalf("decode GeoParquet row %d geometry: %v", index, err)
		}
		point, ok := geometry.(*geom.Point)
		if !ok || math.Abs(point.X()-want[0]) > 1e-6 || math.Abs(point.Y()-want[1]) > 1e-6 || point.SRID() != 0 {
			t.Fatalf("GeoParquet row %d geometry = %#v, want 3857 coordinates %v without embedded SRID", index, geometry, want)
		}
	}
}

func openCRSIntegrationPostgres(t *testing.T, ctx context.Context, pg *postgresql.PostgreSQLPlugin, connInfo engineplugin.ConnectionInfo) *sql.DB {
	t.Helper()
	dsn, err := pg.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN: %v", err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if _, err := db.ExecContext(ctx, "SELECT postgis_version()"); err != nil {
		_ = db.Close()
		t.Skipf("PostGIS is not available: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type crsIntegrationContentWriter struct {
	buffer bytes.Buffer
}

func (w *crsIntegrationContentWriter) Type() string         { return "crs_integration_memory" }
func (w *crsIntegrationContentWriter) DisplayName() string  { return "CRS integration memory" }
func (w *crsIntegrationContentWriter) EngineOrigin() string { return "general" }
func (w *crsIntegrationContentWriter) DefaultPort() int     { return 0 }
func (w *crsIntegrationContentWriter) RequiredFields() []string {
	return nil
}
func (w *crsIntegrationContentWriter) SensitiveFields() []string {
	return nil
}
func (w *crsIntegrationContentWriter) ValidateConnectionInfo(engineplugin.ConnectionInfo) error {
	return nil
}
func (w *crsIntegrationContentWriter) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}
func (w *crsIntegrationContentWriter) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}
func (w *crsIntegrationContentWriter) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}
func (w *crsIntegrationContentWriter) CreateContent(context.Context, engineplugin.ConnectionInfo, engineplugin.CatalogPath, engineplugin.WriteOptions) (io.WriteCloser, error) {
	w.buffer.Reset()
	return &crsIntegrationWriteCloser{Writer: &w.buffer}, nil
}
func (w *crsIntegrationContentWriter) Bytes() []byte {
	return append([]byte(nil), w.buffer.Bytes()...)
}

type crsIntegrationWriteCloser struct {
	io.Writer
}

func (w *crsIntegrationWriteCloser) Close() error { return nil }

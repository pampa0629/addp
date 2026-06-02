package preview

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
	"github.com/addp/manager/internal/models"
)

func TestBuildDatabaseRenderGeometryColumns(t *testing.T) {
	t.Parallel()

	rows := []map[string]interface{}{
		{
			"geom":                  "POINT(1 2)",
			"__render_geojson_geom": `{"type":"Point","coordinates":[1,2]}`,
		},
		{
			"geom":                  nil,
			"__render_geojson_geom": nil,
		},
	}

	got := buildDatabaseRenderGeometryColumns([]string{"geom"}, rows)
	if len(got) != 1 {
		t.Fatalf("expected 1 render geometry mapping, got %d", len(got))
	}
	if got["geom"] != "__render_geojson_geom" {
		t.Fatalf("unexpected render geometry column mapping: %+v", got)
	}
}

func TestBuildDatabaseRenderGeometryColumnsIgnoreInvalidPayload(t *testing.T) {
	t.Parallel()

	rows := []map[string]interface{}{
		{
			"geom":                  "POINT(1 2)",
			"__render_geojson_geom": "not-json",
		},
	}

	got := buildDatabaseRenderGeometryColumns([]string{"geom"}, rows)
	if len(got) != 0 {
		t.Fatalf("expected invalid render payload to be ignored, got %+v", got)
	}
}

func TestDatabasePreviewPostgreSQLPrimaryKeyPageQueryUsesKeyCTEForDeepOffset(t *testing.T) {
	t.Parallel()

	dialect := sqldialect.ForEngine("postgresql")
	columns := []datatype.FieldInfo{
		{Name: "SmID", NativeType: "bigint", PrimaryKey: true},
		{Name: "SmGeometry", NativeType: "geometry(MultiPolygon,2360)"},
		{Name: "DLMC", NativeType: "text"},
	}
	selectExpr := databasePreviewSelectExpr(dialect, columns, databasePreviewSourceAlias)
	query := databasePreviewPostgreSQLPrimaryKeyPageQuery(dialect, selectExpr, "public", "dltb", databasePrimaryKeyColumns(columns), 20, 10000000)

	mustContain := []string{
		`WITH "__addp_page_keys" AS (SELECT "SmID" FROM "public"."dltb" ORDER BY "SmID" LIMIT 20 OFFSET 10000000)`,
		`FROM "public"."dltb" AS "__addp_src" JOIN "__addp_page_keys" AS "__addp_keys" ON "__addp_src"."SmID" = "__addp_keys"."SmID"`,
		`ST_AsText("__addp_src"."SmGeometry") AS "SmGeometry"`,
		`AS "__render_geojson_SmGeometry"`,
		`ORDER BY "__addp_src"."SmID"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(query, want) {
			t.Fatalf("query does not contain %q:\n%s", want, query)
		}
	}
}

func TestDatabasePreviewPostgreSQLPrimaryKeyPageQueryOrdersFirstPage(t *testing.T) {
	t.Parallel()

	dialect := sqldialect.ForEngine("postgresql")
	columns := []datatype.FieldInfo{
		{Name: "id", NativeType: "bigint", PrimaryKey: true},
		{Name: "name", NativeType: "text"},
	}
	selectExpr := databasePreviewSelectExpr(dialect, columns, databasePreviewSourceAlias)
	query := databasePreviewPostgreSQLPrimaryKeyPageQuery(dialect, selectExpr, "public", "cities", databasePrimaryKeyColumns(columns), 50, 0)

	want := `SELECT "__addp_src"."id" AS "id", "__addp_src"."name" AS "name" FROM "public"."cities" AS "__addp_src" ORDER BY "__addp_src"."id" LIMIT 50`
	if query != want {
		t.Fatalf("unexpected query:\n%s\nwant:\n%s", query, want)
	}
}

func TestDatabaseTablePreviewProviderPreviewUsesBatchReadAndAttributeRowCount(t *testing.T) {
	previous, previousErr := plugin.Get("postgresql")
	rowCount := int64(999)
	enginePlugin := &recordingDatabasePreviewPlugin{
		engineType: "postgresql",
		catalogFacts: &plugin.CatalogFacts{
			Table: &datatype.TableInfo{
				RowCount: &rowCount,
				Fields: []datatype.FieldInfo{
					{Name: "SmID", Type: datatype.FieldTypeBigInt, NativeType: "bigint", Nullable: false, PrimaryKey: true},
					{Name: "SmGeometry", Type: datatype.FieldTypeGeometry, NativeType: "geometry(MultiPolygon,2360)", Nullable: true},
					{Name: "DLMC", Type: datatype.FieldTypeString, NativeType: "text", Nullable: true},
				},
			},
		},
		batchData: &plugin.BatchData{
			Rows: []map[string]interface{}{
				{
					"SmID":                        int64(10),
					"SmGeometry":                  "POINT(1 2)",
					"DLMC":                        "test",
					"__render_geojson_SmGeometry": `{"type":"Point","coordinates":[1,2]}`,
				},
			},
		},
	}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	provider := &DatabaseTablePreviewProvider{}
	req := &PreviewRequest{
		Engine: &models.Engine{
			ID:         7,
			EngineType: enginePlugin.Type(),
		},
		Schema:   "public",
		Table:    "public.dltb",
		Page:     3,
		PageSize: 2,
		ProviderPath: plugin.CatalogPath{
			Version:  plugin.CatalogPathVersion,
			EngineID: 7,
			Segments: []plugin.CatalogSegment{
				{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
				{Term: plugin.CatalogTermSchema, Kind: plugin.CatalogKindNamespace, Name: "public"},
				{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: "dltb"},
			},
		},
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": int64(321),
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}

	if preview.Total != 321 {
		t.Fatalf("Total = %d, want 321 from request attributes", preview.Total)
	}
	if len(enginePlugin.readBatchCalls) != 1 {
		t.Fatalf("ReadBatch call count = %d, want 1", len(enginePlugin.readBatchCalls))
	}
	if len(enginePlugin.readBatchPaths) != 1 || !plugin.IsCatalogRootSegment(enginePlugin.readBatchPaths[0].Segments[0]) {
		t.Fatalf("ReadBatch path = %#v, want explicit root segment", enginePlugin.readBatchPaths)
	}
	if len(enginePlugin.describePaths) != 1 || !plugin.IsCatalogRootSegment(enginePlugin.describePaths[0].Segments[0]) {
		t.Fatalf("DescribeCatalogFacts path = %#v, want explicit root segment", enginePlugin.describePaths)
	}
	if got := enginePlugin.readBatchCalls[0].Query; !strings.Contains(got, `WITH "__addp_page_keys"`) {
		t.Fatalf("ReadBatch query does not use page-key CTE:\n%s", got)
	}
	if !strings.Contains(enginePlugin.readBatchCalls[0].Query, `OFFSET 4`) {
		t.Fatalf("ReadBatch query = %q, want offset 4", enginePlugin.readBatchCalls[0].Query)
	}
	if preview.RenderGeometryColumns["SmGeometry"] != "__render_geojson_SmGeometry" {
		t.Fatalf("RenderGeometryColumns = %#v", preview.RenderGeometryColumns)
	}
}

func TestDatabaseTablePreviewProviderPreviewFallsBackToCatalogFactsRowCount(t *testing.T) {
	previous, previousErr := plugin.Get("postgresql")
	rowCount := int64(999)
	enginePlugin := &recordingDatabasePreviewPlugin{
		engineType: "postgresql",
		catalogFacts: &plugin.CatalogFacts{
			Table: &datatype.TableInfo{
				RowCount: &rowCount,
				Fields: []datatype.FieldInfo{
					{Name: "id", Type: datatype.FieldTypeBigInt, NativeType: "bigint", Nullable: false, PrimaryKey: true},
					{Name: "name", Type: datatype.FieldTypeString, NativeType: "text", Nullable: true},
				},
			},
		},
		batchData: &plugin.BatchData{
			Rows: []map[string]interface{}{
				{"id": int64(1), "name": "alice"},
			},
		},
	}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	provider := &DatabaseTablePreviewProvider{}
	req := &PreviewRequest{
		Engine: &models.Engine{
			ID:         8,
			EngineType: enginePlugin.Type(),
		},
		Schema:   "public",
		Table:    "public.people",
		Page:     1,
		PageSize: 10,
		ProviderPath: plugin.CatalogPath{
			Version:  plugin.CatalogPathVersion,
			EngineID: 8,
			Segments: []plugin.CatalogSegment{
				{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
				{Term: plugin.CatalogTermSchema, Kind: plugin.CatalogKindNamespace, Name: "public"},
				{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: "people"},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}

	if preview.Total != 999 {
		t.Fatalf("Total = %d, want 999 from catalog facts", preview.Total)
	}
	if len(enginePlugin.readBatchCalls) != 1 {
		t.Fatalf("ReadBatch call count = %d, want 1", len(enginePlugin.readBatchCalls))
	}
	if enginePlugin.readBatchCalls[0].Query == "" {
		t.Fatalf("ReadBatch query should not be empty")
	}
}

type recordingDatabasePreviewPlugin struct {
	engineType     string
	catalogFacts   *plugin.CatalogFacts
	batchData      *plugin.BatchData
	describePaths  []plugin.CatalogPath
	readBatchPaths []plugin.CatalogPath
	readBatchCalls []plugin.BatchReadOptions
}

func (p *recordingDatabasePreviewPlugin) Type() string         { return p.engineType }
func (p *recordingDatabasePreviewPlugin) DisplayName() string  { return p.engineType }
func (p *recordingDatabasePreviewPlugin) EngineOrigin() string { return "general" }
func (p *recordingDatabasePreviewPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingDatabasePreviewPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingDatabasePreviewPlugin) DefaultPort() int          { return 0 }
func (p *recordingDatabasePreviewPlugin) RequiredFields() []string  { return nil }
func (p *recordingDatabasePreviewPlugin) SensitiveFields() []string { return nil }
func (p *recordingDatabasePreviewPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *recordingDatabasePreviewPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{
		SchemaVersion: plugin.CapabilitiesSchemaVersion,
		EngineType:    p.engineType,
		EngineFamily:  "tabular",
	}
}
func (p *recordingDatabasePreviewPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.TabularCatalogModel("schema")
}
func (p *recordingDatabasePreviewPlugin) DescribeCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	p.describePaths = append(p.describePaths, path)
	if p.catalogFacts == nil {
		return nil, nil
	}
	return p.catalogFacts, nil
}
func (p *recordingDatabasePreviewPlugin) ReadBatch(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	p.readBatchPaths = append(p.readBatchPaths, path)
	p.readBatchCalls = append(p.readBatchCalls, opts)
	if p.batchData == nil {
		return &plugin.BatchData{}, nil
	}
	return p.batchData, nil
}

var _ plugin.EnginePlugin = (*recordingDatabasePreviewPlugin)(nil)
var _ plugin.CatalogModelProvider = (*recordingDatabasePreviewPlugin)(nil)
var _ plugin.CatalogFactsProvider = (*recordingDatabasePreviewPlugin)(nil)
var _ plugin.BatchReadableProvider = (*recordingDatabasePreviewPlugin)(nil)

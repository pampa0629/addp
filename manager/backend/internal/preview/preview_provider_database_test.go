package preview

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/dataprofile"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
	"github.com/addp/manager/internal/models"
)

func TestDatabasePreviewPostgreSQLPrimaryKeyPageQueryUsesKeyCTEForDeepOffset(t *testing.T) {
	t.Parallel()

	dialect := sqldialect.ForEngine("postgresql")
	columns := []datatype.FieldInfo{
		{Name: "SmID", NativeType: "bigint", PrimaryKey: true},
		{Name: "SmGeometry", NativeType: "geometry(MultiPolygon,2360)"},
		{Name: "DLMC", NativeType: "text"},
	}
	selectExpr := databasePreviewSelectExpr(dialect, columns, databasePreviewSourceAlias)
	query := databasePreviewPostgreSQLPrimaryKeyPageQuery(dialect, selectExpr, "public", "dltb", databasePrimaryKeyColumns(columns), "", 20, 10000000)

	mustContain := []string{
		`WITH "__addp_page_keys" AS (SELECT "SmID" FROM "public"."dltb" ORDER BY "SmID" LIMIT 20 OFFSET 10000000)`,
		`FROM "public"."dltb" AS "__addp_src" JOIN "__addp_page_keys" AS "__addp_keys" ON "__addp_src"."SmID" = "__addp_keys"."SmID"`,
		`ST_AsText("__addp_src"."SmGeometry") AS "SmGeometry"`,
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
	query := databasePreviewPostgreSQLPrimaryKeyPageQuery(dialect, selectExpr, "public", "cities", databasePrimaryKeyColumns(columns), "", 50, 0)

	want := `SELECT "__addp_src"."id" AS "id", "__addp_src"."name" AS "name" FROM "public"."cities" AS "__addp_src" ORDER BY "__addp_src"."id" LIMIT 50`
	if query != want {
		t.Fatalf("unexpected query:\n%s\nwant:\n%s", query, want)
	}
}

func TestDatabaseGeometryColumnsUsesCommonSpatialFactsForMySQL(t *testing.T) {
	srid := 4326
	columns := databaseGeometryColumns(&datatype.SpatialInfo{
		GeometryColumns:       []datatype.GeometryColumnInfo{{Name: "shape", GeometryType: "Polygon", SRID: &srid}},
		PrimaryGeometryColumn: "shape",
	}, []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, NativeType: "bigint"},
		{Name: "shape", Type: datatype.FieldTypeGeometry, NativeType: "polygon"},
	})
	if !reflect.DeepEqual(columns, []string{"shape"}) {
		t.Fatalf("geometry columns = %#v", columns)
	}
}

func TestDatabasePreviewMySQLUsesCatalogReadWithGeoJSONHint(t *testing.T) {
	reader := &recordingDatabasePreviewPlugin{engineType: "mysql"}
	provider := &DatabaseTablePreviewProvider{}
	_, err := provider.queryData(context.Background(), reader, nil, plugin.ConnectionInfo{}, plugin.CatalogPath{}, "mysql", "business", "store_locations", 20, 10, nil, dataprofile.DataScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.readBatchCalls) != 1 {
		t.Fatalf("read calls = %d", len(reader.readBatchCalls))
	}
	call := reader.readBatchCalls[0]
	if call.Query != "" || call.Limit != 10 || call.Offset != 20 {
		t.Fatalf("MySQL batch options = %#v", call)
	}
	if call.Hints[plugin.TableReadHintGeometryEncoding] != "geojson" {
		t.Fatalf("MySQL geometry hint = %#v", call.Hints)
	}
}

func TestDatabaseTablePreviewProviderPreviewUsesBatchReadAndAttributeRowCount(t *testing.T) {
	previous, previousErr := plugin.Get("postgresql")
	rowCount := int64(999)
	srid := 2360
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
			Spatial: &datatype.SpatialInfo{
				GeometryColumns: []datatype.GeometryColumnInfo{{
					Name:         "SmGeometry",
					GeometryType: "MultiPolygon",
					SRID:         &srid,
					CRSRef:       "EPSG:2360",
				}},
				PrimaryGeometryColumn: "SmGeometry",
				CRSDefinitions: []datatype.CRSDefinition{{
					ID:                 "EPSG:2360",
					DefinitionEncoding: datatype.CRSDefinitionEncodingWKT,
					Definition:         "PROJCS[...]",
					Source:             datatype.CRSDefinitionSourcePostGISSpatialRefSys,
				}},
			},
		},
		batchData: &plugin.BatchData{
			Rows: []map[string]interface{}{
				{
					"SmID":       int64(10),
					"SmGeometry": "POINT(1 2)",
					"DLMC":       "test",
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
	metaRowCount := int64(321)
	metaTable := datatype.TableInfoPayload(&datatype.TableInfo{
		RowCount: &metaRowCount,
		Fields: []datatype.FieldInfo{
			{Name: "SmID", Type: datatype.FieldTypeBigInt, NativeType: "int8", Nullable: false, PrimaryKey: true},
			{Name: "SmGeometry", Type: datatype.FieldTypeGeometry, NativeType: "geometry", Nullable: true},
			{Name: "DLMC", Type: datatype.FieldTypeString, NativeType: "character varying", Nullable: true},
		},
	})
	req := &PreviewRequest{
		Engine: &models.Engine{
			ID:         7,
			EngineType: enginePlugin.Type(),
		},
		EnginePlugin: enginePlugin,
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
				"table": metaTable,
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
	if preview.GeometryColumn != "SmGeometry" {
		t.Fatalf("GeometryColumn = %q, want SmGeometry", preview.GeometryColumn)
	}
	if len(preview.Fields) != 3 || preview.Fields[1].Name != "SmGeometry" || preview.Fields[1].Type != datatype.FieldTypeGeometry || preview.Fields[2].NativeType != "character varying" {
		t.Fatalf("canonical preview fields = %#v, want fields from Meta attributes", preview.Fields)
	}
	if preview.SourceSRID != 2360 || preview.SourceCRS != "EPSG:2360" {
		t.Fatalf("source CRS = %d/%q, want 2360/EPSG:2360", preview.SourceSRID, preview.SourceCRS)
	}
	if preview.SourceCRSDefinition == nil || preview.SourceCRSDefinition.ID != "EPSG:2360" || preview.SourceCRSDefinition.DefinitionEncoding != datatype.CRSDefinitionEncodingWKT {
		t.Fatalf("source CRS definition = %#v, want EPSG:2360 wkt", preview.SourceCRSDefinition)
	}
	if preview.TransformStatus != "not_transformed" || preview.PreviewHint != "frontend_transform_required" {
		t.Fatalf("transform contract = %q/%q, want not_transformed/frontend_transform_required", preview.TransformStatus, preview.PreviewHint)
	}
	if strings.Contains(enginePlugin.readBatchCalls[0].Query, "ST_Transform") {
		t.Fatalf("ReadBatch query should not transform geometry:\n%s", enginePlugin.readBatchCalls[0].Query)
	}
}

func TestDatabaseTablePreviewProviderBindsProfileConditionsBeforePaging(t *testing.T) {
	reader := &recordingDatabasePreviewPlugin{engineType: "postgresql", batchData: &plugin.BatchData{}}
	provider := &DatabaseTablePreviewProvider{}
	_, err := provider.queryData(
		context.Background(), reader, nil, plugin.ConnectionInfo{},
		plugin.TabularItemPath(7, plugin.CatalogTermSchema, "public", "orders"),
		"postgresql", "public", "orders", 0, 500,
		[]datatype.FieldInfo{{Name: "status", Type: datatype.FieldTypeString}},
		dataprofile.DataScope{
			Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd,
			Conditions: []dataprofile.DataScopeCondition{{Field: "status", Operator: "eq", Value: "active"}},
		},
	)
	if err != nil {
		t.Fatalf("queryData() error = %v", err)
	}
	call := reader.readBatchCalls[0]
	if !strings.Contains(call.Query, `WHERE ("status" = ?)`) || strings.Contains(call.Query, "active") {
		t.Fatalf("parameterized query = %q", call.Query)
	}
	if !reflect.DeepEqual(call.Args, []interface{}{"active"}) {
		t.Fatalf("query args = %#v", call.Args)
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
		EnginePlugin: enginePlugin,
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

func TestDatabaseTablePreviewProviderAllowsQuickViewPageSize(t *testing.T) {
	previous, previousErr := plugin.Get("postgresql")
	rowCount := int64(127)
	enginePlugin := &recordingDatabasePreviewPlugin{
		engineType: "postgresql",
		catalogFacts: &plugin.CatalogFacts{
			Table: &datatype.TableInfo{
				RowCount: &rowCount,
				Fields: []datatype.FieldInfo{
					{Name: "id", Type: datatype.FieldTypeBigInt, NativeType: "bigint", Nullable: false, PrimaryKey: true},
					{Name: "geometry", Type: datatype.FieldTypeGeometry, NativeType: "geometry(MultiPolygon,4326)", Nullable: true},
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
			ID:         8,
			EngineType: enginePlugin.Type(),
		},
		EnginePlugin: enginePlugin,
		Schema:   "public",
		Table:    "public.farmland",
		Page:     1,
		PageSize: 127,
		ProviderPath: plugin.CatalogPath{
			Version:  plugin.CatalogPathVersion,
			EngineID: 8,
			Segments: []plugin.CatalogSegment{
				{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
				{Term: plugin.CatalogTermSchema, Kind: plugin.CatalogKindNamespace, Name: "public"},
				{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: "farmland"},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.PageSize != 127 {
		t.Fatalf("PageSize = %d, want 127", preview.PageSize)
	}
	if len(enginePlugin.readBatchCalls) != 1 {
		t.Fatalf("ReadBatch call count = %d, want 1", len(enginePlugin.readBatchCalls))
	}
	if !strings.Contains(enginePlugin.readBatchCalls[0].Query, "LIMIT 127") {
		t.Fatalf("ReadBatch query = %q, want LIMIT 127", enginePlugin.readBatchCalls[0].Query)
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

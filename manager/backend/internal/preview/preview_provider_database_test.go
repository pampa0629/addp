package preview

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
)

func TestDatabasePreviewPostgreSQLPrimaryKeyPageQueryUsesKeyCTEForDeepOffset(t *testing.T) {
	t.Parallel()

	dialect := commonquery.ForDialect(commonquery.DialectPostgreSQL)
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

	dialect := commonquery.ForDialect(commonquery.DialectPostgreSQL)
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

func TestDatabasePreviewOracleSpatialSelectUsesWKT(t *testing.T) {
	query := databasePreviewSelectExpr(commonquery.ForDialect(commonquery.DialectOracle), []datatype.FieldInfo{
		{Name: "ID", NativeType: "NUMBER(10,0)"},
		{Name: "SHAPE", Type: datatype.FieldTypeGeometry, NativeType: "MDSYS.SDO_GEOMETRY"},
	}, "")
	if !strings.Contains(query, `SDO_UTIL.TO_WKTGEOMETRY("SHAPE") AS "SHAPE"`) {
		t.Fatalf("Oracle spatial select = %s", query)
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
	_, err := provider.queryData(context.Background(), reader, nil, plugin.ConnectionInfo{}, plugin.EngineCatalogPath{}, "mysql", "business", "store_locations", 20, 10, nil, dataprofile.DataScope{})
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

func TestDatabasePreviewOracleUsesTableReadSessionForSpatialRows(t *testing.T) {
	reader := &recordingDatabasePreviewPlugin{
		engineType:  "oracle",
		sessionData: &plugin.BatchData{Rows: []map[string]interface{}{{"ID": int64(1), "SHAPE": []byte{1, 2, 3}}}},
	}
	provider := &DatabaseTablePreviewProvider{}
	rows, err := provider.queryData(
		context.Background(), reader, reader, plugin.ConnectionInfo{},
		plugin.TabularItemPath(22, plugin.EngineCatalogTermSchema, "BUSINESS", "CUSTOMER_LOCATIONS"),
		"oracle", "BUSINESS", "CUSTOMER_LOCATIONS", 0, 20,
		[]datatype.FieldInfo{
			{Name: "ID", Type: datatype.FieldTypeBigInt},
			{Name: "SHAPE", Type: datatype.FieldTypeGeometry, NativeType: "MDSYS.SDO_GEOMETRY"},
		}, dataprofile.DataScope{},
	)
	if err != nil {
		t.Fatalf("queryData() error = %v", err)
	}
	if len(rows) != 1 || len(reader.openSessionCalls) != 1 {
		t.Fatalf("rows=%#v open_session_calls=%d", rows, len(reader.openSessionCalls))
	}
	if len(reader.readBatchCalls) != 0 {
		t.Fatalf("Oracle spatial preview must not use raw ReadBatch: %#v", reader.readBatchCalls)
	}
	if got := reader.openSessionCalls[0].Hints[plugin.TableReadHintGeometryEncoding]; got != "ewkb" {
		t.Fatalf("Oracle table session geometry hint = %#v, want ewkb", got)
	}
}

func TestDatabaseTablePreviewProviderPreviewUsesPreparedQueryAndAttributeRowCount(t *testing.T) {
	previous, previousErr := plugin.Get("postgresql")
	rowCount := int64(999)
	srid := 2360
	enginePlugin := &recordingDatabasePreviewPlugin{
		engineType: "postgresql",
		catalogFacts: &plugin.EngineCatalogFacts{
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
		EnginePlugin:    enginePlugin,
		executePrepared: executeDatabasePreviewTestPlan,
		Schema:          "public",
		Table:           "public.dltb",
		Page:            3,
		PageSize:        2,
		ProviderPath: plugin.EngineCatalogPath{
			Version:  plugin.EngineCatalogPathVersion,
			EngineID: 7,
			Segments: []plugin.EngineCatalogSegment{
				{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
				{Term: plugin.EngineCatalogTermSchema, Kind: plugin.EngineCatalogKindNamespace, Name: "public"},
				{Term: plugin.EngineCatalogTermTable, Kind: plugin.EngineCatalogKindTable, Name: "dltb"},
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

	if preview.Total == nil || *preview.Total != 321 {
		t.Fatalf("Total = %v, want 321 from request attributes", preview.Total)
	}
	if len(enginePlugin.readBatchCalls) != 0 || len(enginePlugin.prepareCalls) != 1 || enginePlugin.executeCalls != 1 {
		t.Fatalf("reads=%d prepare=%d execute=%d", len(enginePlugin.readBatchCalls), len(enginePlugin.prepareCalls), enginePlugin.executeCalls)
	}
	if path := enginePlugin.prepareCalls[0].TargetPath; path == nil || !reflect.DeepEqual(*path, req.ProviderPath) {
		t.Fatalf("prepared path = %#v, want resolved path", path)
	}
	if len(enginePlugin.describePaths) != 1 || !plugin.IsEngineCatalogRootSegment(enginePlugin.describePaths[0].Segments[0]) {
		t.Fatalf("DescribeEngineCatalogFacts path = %#v, want explicit root segment", enginePlugin.describePaths)
	}
	if got := enginePlugin.prepareCalls[0].Query; !strings.Contains(got, `WITH "__addp_page_keys"`) {
		t.Fatalf("prepared query does not use page-key CTE:\n%s", got)
	}
	if !strings.Contains(enginePlugin.prepareCalls[0].Query, `OFFSET 4`) {
		t.Fatalf("prepared query = %q, want offset 4", enginePlugin.prepareCalls[0].Query)
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
	if strings.Contains(enginePlugin.prepareCalls[0].Query, "ST_Transform") {
		t.Fatalf("prepared query should not transform geometry:\n%s", enginePlugin.prepareCalls[0].Query)
	}
}

func TestDatabaseTablePreviewProviderBindsProfileConditionsBeforePaging(t *testing.T) {
	reader := &recordingDatabasePreviewPlugin{engineType: "postgresql", batchData: &plugin.BatchData{}}
	provider := &DatabaseTablePreviewProvider{}
	_, err := provider.preparePostgreSQLPreview(context.Background(), &PreviewRequest{
		Engine: &models.Engine{ID: 7}, EnginePlugin: reader,
		ProviderPath: plugin.TabularItemPath(7, plugin.EngineCatalogTermSchema, "public", "orders"),
		DataScope: dataprofile.DataScope{
			Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd,
			Conditions: []dataprofile.DataScopeCondition{{Field: "status", Operator: "eq", Value: "active"}},
		},
	}, []datatype.FieldInfo{{Name: "status", Type: datatype.FieldTypeString}}, 0, 500)
	if err != nil {
		t.Fatalf("queryData() error = %v", err)
	}
	call := reader.prepareCalls[0]
	if !strings.Contains(call.Query, `WHERE ("status" = $1)`) || strings.Contains(call.Query, "active") {
		t.Fatalf("parameterized query = %q", call.Query)
	}
	if !reflect.DeepEqual(call.Options.Args, []interface{}{"active"}) || !call.Options.ReadOnly || reader.executeCalls != 0 || len(reader.readBatchCalls) != 0 {
		t.Fatalf("query options = %#v, execute=%d reads=%d", call.Options, reader.executeCalls, len(reader.readBatchCalls))
	}
}

func TestDatabaseTablePreviewProviderPreviewFallsBackToCatalogFactsRowCount(t *testing.T) {
	previous, previousErr := plugin.Get("postgresql")
	rowCount := int64(999)
	enginePlugin := &recordingDatabasePreviewPlugin{
		engineType: "postgresql",
		catalogFacts: &plugin.EngineCatalogFacts{
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
		EnginePlugin:    enginePlugin,
		executePrepared: executeDatabasePreviewTestPlan,
		Schema:          "public",
		Table:           "public.people",
		Page:            1,
		PageSize:        10,
		ProviderPath: plugin.EngineCatalogPath{
			Version:  plugin.EngineCatalogPathVersion,
			EngineID: 8,
			Segments: []plugin.EngineCatalogSegment{
				{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
				{Term: plugin.EngineCatalogTermSchema, Kind: plugin.EngineCatalogKindNamespace, Name: "public"},
				{Term: plugin.EngineCatalogTermTable, Kind: plugin.EngineCatalogKindTable, Name: "people"},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}

	if preview.Total == nil || *preview.Total != 999 {
		t.Fatalf("Total = %v, want 999 from catalog facts", preview.Total)
	}
	if len(enginePlugin.readBatchCalls) != 0 || len(enginePlugin.prepareCalls) != 1 || enginePlugin.executeCalls != 1 {
		t.Fatalf("reads=%d prepare=%d execute=%d", len(enginePlugin.readBatchCalls), len(enginePlugin.prepareCalls), enginePlugin.executeCalls)
	}
}

func TestDatabaseTablePreviewProviderAllowsQuickViewPageSize(t *testing.T) {
	previous, previousErr := plugin.Get("postgresql")
	rowCount := int64(127)
	enginePlugin := &recordingDatabasePreviewPlugin{
		engineType: "postgresql",
		catalogFacts: &plugin.EngineCatalogFacts{
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
		EnginePlugin:    enginePlugin,
		executePrepared: executeDatabasePreviewTestPlan,
		Schema:          "public",
		Table:           "public.farmland",
		Page:            1,
		PageSize:        127,
		ProviderPath: plugin.EngineCatalogPath{
			Version:  plugin.EngineCatalogPathVersion,
			EngineID: 8,
			Segments: []plugin.EngineCatalogSegment{
				{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
				{Term: plugin.EngineCatalogTermSchema, Kind: plugin.EngineCatalogKindNamespace, Name: "public"},
				{Term: plugin.EngineCatalogTermTable, Kind: plugin.EngineCatalogKindTable, Name: "farmland"},
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
	if len(enginePlugin.readBatchCalls) != 0 || len(enginePlugin.prepareCalls) != 1 || enginePlugin.executeCalls != 1 {
		t.Fatalf("reads=%d prepare=%d execute=%d", len(enginePlugin.readBatchCalls), len(enginePlugin.prepareCalls), enginePlugin.executeCalls)
	}
	if !strings.Contains(enginePlugin.prepareCalls[0].Query, "LIMIT 127") {
		t.Fatalf("prepared query = %q, want LIMIT 127", enginePlugin.prepareCalls[0].Query)
	}
}

func TestDatabasePreviewPostgreSQLRejectsMissingAuthorizationExecutorAndOldReadPath(t *testing.T) {
	reader := &recordingDatabasePreviewPlugin{engineType: "postgresql"}
	provider := &DatabaseTablePreviewProvider{}
	_, err := provider.Preview(context.Background(), &PreviewRequest{EnginePlugin: reader})
	if !errors.Is(err, ErrSourceAuthorizationRequired) || len(reader.describePaths) != 0 || len(reader.prepareCalls) != 0 {
		t.Fatalf("unchecked provider was accessed: %v %#v", err, reader)
	}
	_, err = provider.queryData(context.Background(), reader, nil, nil, plugin.EngineCatalogPath{}, "postgresql", "public", "orders", 0, 20, nil, dataprofile.DataScope{})
	if !errors.Is(err, ErrSourceAuthorizationRequired) || len(reader.readBatchCalls) != 0 {
		t.Fatalf("old PostgreSQL read path remains open: %v", err)
	}
}

func TestDatabasePreviewPostgreSQLUsesExactLeafName(t *testing.T) {
	reader := &recordingDatabasePreviewPlugin{engineType: "postgresql"}
	_, err := (&DatabaseTablePreviewProvider{}).preparePostgreSQLPreview(context.Background(), &PreviewRequest{
		Engine: &models.Engine{ID: 7}, EnginePlugin: reader, Schema: "public", Table: "public.fake",
		ProviderPath: plugin.TabularItemPath(7, plugin.EngineCatalogTermSchema, "public", "public.real.name"),
	}, []datatype.FieldInfo{{Name: "id"}}, 0, 20)
	if err != nil || !strings.Contains(reader.prepareCalls[0].Query, `"public"."public.real.name"`) {
		t.Fatalf("leaf name was guessed from UI name: %v %#v", err, reader.prepareCalls)
	}
}

func executeDatabasePreviewTestPlan(ctx context.Context, _ plugin.EnginePlugin, plan plugin.PreparedQuery) (*plugin.QueryResult, *dataprotection.PreparedTableProtection, error) {
	result, err := plan.Execute(ctx)
	return result, &dataprotection.PreparedTableProtection{Apply: func(*plugin.QueryResult) error { return nil }}, err
}

type recordingDatabasePreviewPlugin struct {
	engineType       string
	catalogFacts     *plugin.EngineCatalogFacts
	batchData        *plugin.BatchData
	sessionData      *plugin.BatchData
	describePaths    []plugin.EngineCatalogPath
	readBatchPaths   []plugin.EngineCatalogPath
	readBatchCalls   []plugin.BatchReadOptions
	openSessionCalls []plugin.TableReadSessionOptions
	prepareCalls     []plugin.QueryRequest
	executeCalls     int
}

func (*recordingDatabasePreviewPlugin) QueryLanguages() []string { return []string{"sql"} }
func (*recordingDatabasePreviewPlugin) GenerateSampleQuery(context.Context, plugin.ConnectionInfo, plugin.SampleQueryOptions) (string, string) {
	return "", "sql"
}
func (p *recordingDatabasePreviewPlugin) PrepareQuery(_ context.Context, _ plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	p.prepareCalls = append(p.prepareCalls, req)
	return plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageComplete}, func(context.Context) (*plugin.QueryReadSet, error) {
		return plugin.NewQueryReadSet(*req.TargetPath)
	}, nil, func(context.Context) (*plugin.QueryResult, error) {
		p.executeCalls++
		result := &plugin.QueryResult{}
		if p.batchData != nil {
			result.Rows = p.batchData.Rows
		}
		if p.catalogFacts != nil && p.catalogFacts.Table != nil {
			for _, field := range p.catalogFacts.Table.Fields {
				result.Columns = append(result.Columns, field.Name)
			}
		}
		return result, nil
	})
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
func (p *recordingDatabasePreviewPlugin) SQLDialect() string { return "postgresql" }
func (p *recordingDatabasePreviewPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{
		SchemaVersion: plugin.CapabilitiesSchemaVersion,
		EngineType:    p.engineType,
		EngineFamily:  "tabular",
	}
}
func (p *recordingDatabasePreviewPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel("schema")
}
func (p *recordingDatabasePreviewPlugin) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.describePaths = append(p.describePaths, path)
	if p.catalogFacts == nil {
		return nil, nil
	}
	return p.catalogFacts, nil
}
func (p *recordingDatabasePreviewPlugin) ReadBatch(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	p.readBatchPaths = append(p.readBatchPaths, path)
	p.readBatchCalls = append(p.readBatchCalls, opts)
	if p.batchData == nil {
		return &plugin.BatchData{}, nil
	}
	return p.batchData, nil
}

func (p *recordingDatabasePreviewPlugin) OpenTableReadSession(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	p.openSessionCalls = append(p.openSessionCalls, opts)
	return &recordingTableReadSession{data: p.sessionData}, nil
}

type recordingTableReadSession struct {
	data *plugin.BatchData
	used bool
}

func (s *recordingTableReadSession) ReadBatch(context.Context, int) (*plugin.BatchData, error) {
	if s.data == nil || s.used {
		return &plugin.BatchData{}, nil
	}
	s.used = true
	return s.data, nil
}

func (*recordingTableReadSession) Close(context.Context) error { return nil }

var _ plugin.EnginePlugin = (*recordingDatabasePreviewPlugin)(nil)
var _ plugin.EngineCatalogModelProvider = (*recordingDatabasePreviewPlugin)(nil)
var _ plugin.EngineCatalogFactsProvider = (*recordingDatabasePreviewPlugin)(nil)
var _ plugin.BatchReadableProvider = (*recordingDatabasePreviewPlugin)(nil)
var _ plugin.TableReadSessionProvider = (*recordingDatabasePreviewPlugin)(nil)

package scanruntime

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func TestRefreshKnownTabularItemUsesCatalogFactsWithoutContentReader(t *testing.T) {
	t.Parallel()

	db := openObjectCatalogScanTestDB(t)
	tenantID := uint(1)
	engineID := uint(88)
	srid := 4549
	nullable := true
	rowCount := int64(12)
	enginePlugin := &catalogFactsOnlyTablePlugin{
		engineType: "known-refresh-mysql-spatial-table-test",
		facts: &plugin.EngineCatalogFacts{
			Table: &datatype.TableInfo{
				Name:     "roads",
				Kind:     "table",
				RowCount: &rowCount,
				Fields: []datatype.FieldInfo{
					{Name: "id", NativeType: "bigint", PrimaryKey: true},
					{Name: "geom", NativeType: "geometry", Type: datatype.FieldTypeGeometry},
				},
			},
			Spatial: &datatype.SpatialInfo{
				SRID:   &srid,
				CRSRef: datatype.EPSGCRSRef(srid),
				GeometryColumns: []datatype.GeometryColumnInfo{{
					Name:         "geom",
					GeometryType: "MultiPolygon",
					SRID:         &srid,
					CRSRef:       datatype.EPSGCRSRef(srid),
					Nullable:     &nullable,
				}},
				PrimaryGeometryColumn: "geom",
				CRSDefinitions: []datatype.CRSDefinition{{
					ID:                 datatype.EPSGCRSRef(srid),
					DefinitionEncoding: datatype.CRSDefinitionEncodingWKT,
					Definition:         `PROJCS["CGCS2000_3_Degree_GK_CM_117E"]`,
					Source:             datatype.CRSDefinitionSourceMySQLSpatialRefSys,
				}},
			},
		},
	}
	pluginRegisterForTest(t, enginePlugin)

	repo := metaRepo.NewScanRepository(db)
	parentNode := models.MetaNode{
		TenantID:   tenantID,
		EngineID:   engineID,
		NodeType:   plugin.EngineCatalogTermSchema,
		Name:       "public",
		FullName:   "public",
		Depth:      1,
		Attributes: models.JSONMap{},
	}
	if err := db.Create(&parentNode).Error; err != nil {
		t.Fatalf("create parent node: %v", err)
	}
	item := models.MetaItem{
		TenantID:    tenantID,
		EngineID:    engineID,
		NodeID:      parentNode.ID,
		ItemType:    plugin.EngineCatalogTermTable,
		Name:        "roads",
		FullName:    "public.roads",
		Fingerprint: "known-refresh-table",
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout":    "single",
				"data_type": string(datatype.Table),
			},
			"storage": map[string]interface{}{
				"schema_name": "public",
			},
			"capabilities": map[string]interface{}{
				"spatial": map[string]interface{}{
					"srid": srid,
				},
			},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	runtime := NewItemRefreshRuntime(repo, nil, nil)
	result, err := runtime.RefreshKnownItem(context.Background(), &commonModels.Engine{
		ID:             engineID,
		TenantID:       &tenantID,
		EngineType:     enginePlugin.Type(),
		LifecycleState: "active",
	}, tenantID, item, parentNode)
	if err != nil {
		t.Fatalf("RefreshKnownItem() error = %v", err)
	}
	if result.Fields != 2 {
		t.Fatalf("Fields = %d, want 2", result.Fields)
	}

	var refreshed models.MetaItem
	if err := db.First(&refreshed, item.ID).Error; err != nil {
		t.Fatalf("load refreshed item: %v", err)
	}
	spatial := commonJSON.Section(refreshed.Attributes, "capabilities.spatial")
	if commonJSON.InterfaceString(spatial["crs_ref"]) != datatype.EPSGCRSRef(srid) {
		t.Fatalf("capabilities.spatial.crs_ref = %#v", spatial["crs_ref"])
	}
	definitions := commonJSON.InterfaceSlice(spatial["crs_definitions"])
	if len(definitions) != 1 {
		t.Fatalf("capabilities.spatial.crs_definitions = %#v", definitions)
	}
	definition := commonJSON.InterfaceMap(definitions[0])
	if commonJSON.InterfaceString(definition["definition_encoding"]) != datatype.CRSDefinitionEncodingWKT {
		t.Fatalf("definition_encoding = %#v", definition["definition_encoding"])
	}
	if commonJSON.InterfaceString(definition["source"]) != datatype.CRSDefinitionSourceMySQLSpatialRefSys {
		t.Fatalf("definition source = %#v", definition["source"])
	}
	tableInfo := datatype.TableInfoFromPayload(commonJSON.Section(refreshed.Attributes, "type_info.table"), "roads")
	if tableInfo == nil || len(tableInfo.Fields) != 2 {
		t.Fatalf("type_info.table = %#v", commonJSON.Section(refreshed.Attributes, "type_info.table"))
	}
}

func TestRefreshKnownDynamicSchemaItemUsesSamplingProviderWithoutContentReader(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	tenantID := uint(1)
	engineID := uint(92)
	rowCount := int64(7)
	sizeBytes := int64(2048)
	enginePlugin := &knownRefreshDynamicSchemaPlugin{facts: &plugin.EngineCatalogFacts{
		Table: &datatype.TableInfo{
			Name:      "Persons",
			Kind:      plugin.EngineCatalogKindCollection,
			RowCount:  &rowCount,
			SizeBytes: &sizeBytes,
			Fields: []datatype.FieldInfo{
				{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON},
				{Name: "userInfo.nickName", Path: []string{"userInfo", "nickName"}, Type: datatype.FieldTypeString},
				{Name: "entriedOutdoors", Path: []string{"entriedOutdoors"}, Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON},
			},
			Native: map[string]interface{}{"schema_type": "dynamic", "sample_size": 100},
		},
	}}

	parentNode := models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: plugin.EngineCatalogTermDatabase, Name: "Outdoor", FullName: "Outdoor", Attributes: models.JSONMap{}}
	if err := db.Create(&parentNode).Error; err != nil {
		t.Fatalf("create parent node: %v", err)
	}
	item := models.MetaItem{
		TenantID: tenantID, EngineID: engineID, NodeID: parentNode.ID,
		ItemType: plugin.EngineCatalogTermCollection, Name: "Persons", FullName: "Outdoor.Persons",
		Fingerprint: "known-refresh-dynamic-schema", Attributes: models.JSONMap{"stale": true},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	runtime := NewItemRefreshRuntime(metaRepo.NewScanRepository(db), nil, nil)
	result, err := runtime.RefreshKnownItemWithPlugin(context.Background(), enginePlugin, &commonModels.Engine{
		ID: engineID, TenantID: &tenantID, EngineType: enginePlugin.Type(), LifecycleState: "active",
	}, tenantID, item, parentNode)
	if err != nil {
		t.Fatalf("RefreshKnownItemWithPlugin() error = %v", err)
	}
	if result.Fields != 3 {
		t.Fatalf("Fields = %d, want 3", result.Fields)
	}
	if got := enginePlugin.sampledPath.StringPath(); got != "Outdoor/Persons" {
		t.Fatalf("sampled path = %q, want Outdoor/Persons", got)
	}
	if !enginePlugin.sampledOptions.IncludeSamples || !enginePlugin.sampledOptions.IncludeStatistics ||
		!enginePlugin.sampledOptions.IncludeIndexes || enginePlugin.sampledOptions.SampleSize != 100 {
		t.Fatalf("sample options = %#v", enginePlugin.sampledOptions)
	}

	var refreshed models.MetaItem
	if err := db.First(&refreshed, item.ID).Error; err != nil {
		t.Fatalf("load refreshed item: %v", err)
	}
	if _, exists := refreshed.Attributes["stale"]; exists {
		t.Fatalf("stale attributes survived refresh: %#v", refreshed.Attributes)
	}
	tableInfo := datatype.TableInfoFromPayload(commonJSON.Section(refreshed.Attributes, "type_info.table"), "Persons")
	if tableInfo == nil || len(tableInfo.Fields) != 3 || tableInfo.GetField("userInfo.nickName") == nil {
		t.Fatalf("type_info.table = %#v", commonJSON.Section(refreshed.Attributes, "type_info.table"))
	}
	if got := commonJSON.String(refreshed.Attributes, "capabilities.statistics", "schema_type"); got != "dynamic" {
		t.Fatalf("schema_type = %q, want dynamic", got)
	}
}

func TestRefreshKnownDirectLeafItemUsesCatalogFactsWithoutContentReader(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	tenantID := uint(1)
	engineID := uint(91)
	enginePlugin := &catalogFactsOnlyDirectLeafPlugin{
		engineType: "known-refresh-topic-test",
		facts: &plugin.EngineCatalogFacts{
			Kind: "topic",
			Topic: &plugin.TopicFacts{
				PartitionCount:    1,
				ReplicationFactor: 1,
				Partitions: []plugin.TopicPartitionFacts{{
					Partition:      0,
					Leader:         1,
					Replicas:       []int32{1},
					ISR:            []int32{1},
					EarliestOffset: 10,
					LatestOffset:   20,
				}},
			},
		},
	}
	pluginRegisterForTest(t, enginePlugin)

	parentNode := models.MetaNode{
		TenantID:   tenantID,
		EngineID:   engineID,
		NodeType:   plugin.EngineCatalogTermService,
		Name:       "Business Kafka",
		FullName:   "",
		Attributes: models.JSONMap{},
	}
	if err := db.Create(&parentNode).Error; err != nil {
		t.Fatalf("create parent node: %v", err)
	}
	item := models.MetaItem{
		TenantID:    tenantID,
		EngineID:    engineID,
		NodeID:      parentNode.ID,
		ItemType:    "topic",
		Name:        "orders",
		FullName:    "orders",
		Fingerprint: "known-refresh-topic",
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout":    "single",
				"data_type": string(datatype.Unknown),
			},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	runtime := NewItemRefreshRuntime(metaRepo.NewScanRepository(db), nil, nil)
	result, err := runtime.RefreshKnownItem(context.Background(), &commonModels.Engine{
		ID:             engineID,
		TenantID:       &tenantID,
		EngineType:     enginePlugin.Type(),
		LifecycleState: "active",
	}, tenantID, item, parentNode)
	if err != nil {
		t.Fatalf("RefreshKnownItem() error = %v", err)
	}
	if result.Item == nil || result.Item.ScannedDepth != models.ScannedDepthDeep {
		t.Fatalf("refreshed item = %#v, want deep scan", result.Item)
	}
	if len(enginePlugin.paths) != 1 {
		t.Fatalf("DescribeEngineCatalogFacts call count = %d, want 1", len(enginePlugin.paths))
	}
	path := enginePlugin.paths[0]
	if path.EngineID != engineID || len(path.Segments) != 2 || !plugin.IsEngineCatalogRootSegment(path.Segments[0]) || path.Segments[1].Term != "topic" || path.Segments[1].Name != "orders" {
		t.Fatalf("DescribeEngineCatalogFacts path = %#v", path)
	}
	if got := commonJSON.String(result.Item.Attributes, "item", "data_type"); got != string(datatype.Unknown) {
		t.Fatalf("item.data_type = %q, want unknown", got)
	}
	if _, ok := result.Item.Attributes["type_info"]; ok {
		t.Fatalf("topic runtime facts must not be persisted: %#v", result.Item.Attributes)
	}
}

func TestRefreshKnownSingleOSGBItemRedetectsStaleGLBFormat(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	tenantID := uint(1)
	engineID := uint(89)
	physicalPath := "3d/single-osgb/Tile_4_L20_00010t3.osgb"
	reader := knownRefreshOSGBContentReader{
		staticObjectContentReader: staticObjectContentReader{content: string([]byte{0x00, 0x01, 0x02})},
	}
	pluginRegisterForTest(t, reader)

	parentNode := models.MetaNode{
		TenantID:   tenantID,
		EngineID:   engineID,
		NodeType:   plugin.EngineCatalogTermDirectory,
		Name:       "single-osgb",
		FullName:   "3d/single-osgb",
		Depth:      2,
		Attributes: models.JSONMap{},
	}
	if err := db.Create(&parentNode).Error; err != nil {
		t.Fatalf("create parent node: %v", err)
	}
	sizeBytes := int64(3)
	item := models.MetaItem{
		TenantID:    tenantID,
		EngineID:    engineID,
		NodeID:      parentNode.ID,
		ItemType:    plugin.EngineCatalogTermFile,
		Name:        "Tile_4_L20_00010t3.osgb",
		FullName:    physicalPath,
		Fingerprint: "known-refresh-single-osgb-stale-glb",
		SizeBytes:   &sizeBytes,
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout":    string(format.LayoutSingle),
				"data_type": string(datatype.Model3D),
				"format":    string(format.FormatGLB),
			},
			"storage": map[string]interface{}{
				"physical_path": physicalPath,
				"path":          "3d/single-osgb",
				"name":          "Tile_4_L20_00010t3.osgb",
				"content_type":  "application/octet-stream",
			},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	runtime := NewItemRefreshRuntime(metaRepo.NewScanRepository(db), nil, nil)
	_, err := runtime.RefreshKnownItem(context.Background(), &commonModels.Engine{
		ID:             engineID,
		TenantID:       &tenantID,
		EngineType:     reader.Type(),
		LifecycleState: "active",
	}, tenantID, item, parentNode)
	if err != nil {
		t.Fatalf("RefreshKnownItem() error = %v", err)
	}

	var refreshed models.MetaItem
	if err := db.First(&refreshed, item.ID).Error; err != nil {
		t.Fatalf("load refreshed item: %v", err)
	}
	if got := commonJSON.String(refreshed.Attributes, "item", "format"); got != string(format.FormatOSGB) {
		t.Fatalf("item.format = %q, want osgb", got)
	}
	if got := commonJSON.String(refreshed.Attributes, "item", "data_type"); got != string(datatype.Model3D) {
		t.Fatalf("item.data_type = %q, want model_3d", got)
	}
	if got := commonJSON.String(refreshed.Attributes, "storage", "physical_path"); got != physicalPath {
		t.Fatalf("storage.physical_path = %q, want %q", got, physicalPath)
	}
}

func TestRefreshKnownWholeOSGBSceneRedetectsStaleOSGBFormat(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	tenantID := uint(1)
	engineID := uint(90)
	scopePath := "3d/baita"
	reader := knownRefreshOSGBSceneProvider{
		contents: map[string]string{
			scopePath + "/metadata.xml":                  `<ModelMetadata version="1"><SRS>EPSG:4549</SRS><SRSOrigin>381180,4897399,0</SRSOrigin><Texture><ColorSource>Visible</ColorSource></Texture></ModelMetadata>`,
			scopePath + "/Data/Tile_1/Tile_1_L14_0.osgb": string([]byte{0x00}),
		},
	}
	pluginRegisterForTest(t, reader)

	parentNode := models.MetaNode{
		TenantID:   tenantID,
		EngineID:   engineID,
		NodeType:   plugin.EngineCatalogTermDirectory,
		Name:       "3d",
		FullName:   "3d",
		Depth:      1,
		Attributes: models.JSONMap{},
	}
	if err := db.Create(&parentNode).Error; err != nil {
		t.Fatalf("create parent node: %v", err)
	}
	sizeBytes := int64(2)
	item := models.MetaItem{
		TenantID:    tenantID,
		EngineID:    engineID,
		NodeID:      parentNode.ID,
		ItemType:    plugin.EngineCatalogTermFile,
		Name:        "baita",
		FullName:    scopePath,
		Fingerprint: "known-refresh-whole-osgb-stale-format",
		SizeBytes:   &sizeBytes,
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout":    string(format.LayoutWhole),
				"data_type": string(datatype.Model3D),
				"format":    string(format.FormatOSGB),
			},
			"storage": map[string]interface{}{
				"physical_path": scopePath + "/metadata.xml",
				"path":          "3d/",
				"name":          "baita",
			},
			"format_info": map[string]interface{}{
				string(format.FormatOSGB): map[string]interface{}{"stale": true},
			},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	runtime := NewItemRefreshRuntime(metaRepo.NewScanRepository(db), nil, nil)
	_, err := runtime.RefreshKnownItem(context.Background(), &commonModels.Engine{
		ID:             engineID,
		TenantID:       &tenantID,
		EngineType:     reader.Type(),
		LifecycleState: "active",
	}, tenantID, item, parentNode)
	if err != nil {
		t.Fatalf("RefreshKnownItem() error = %v", err)
	}

	var refreshed models.MetaItem
	if err := db.First(&refreshed, item.ID).Error; err != nil {
		t.Fatalf("load refreshed item: %v", err)
	}
	if got := commonJSON.String(refreshed.Attributes, "item", "format"); got != string(format.FormatOSGBScene) {
		t.Fatalf("item.format = %q, want osgb_scene", got)
	}
	if got := commonJSON.String(refreshed.Attributes, "storage", "physical_path"); got != scopePath {
		t.Fatalf("storage.physical_path = %q, want %q", got, scopePath)
	}
	if got := commonJSON.String(refreshed.Attributes, "type_info.model_3d", "model_kind"); got != datatype.Model3DKindPhotogrammetryScene {
		t.Fatalf("type_info.model_3d.model_kind = %q, want photogrammetry_scene", got)
	}
	if got := commonJSON.Section(refreshed.Attributes, "format_info.osgb_scene"); got["manifest_ref"] != "metadata.xml" {
		t.Fatalf("format_info.osgb_scene = %#v, want manifest metadata.xml", got)
	}
	if stale := commonJSON.Section(refreshed.Attributes, "format_info.osgb"); len(stale) != 0 {
		t.Fatalf("format_info.osgb = %#v, want stale OSGB info removed", stale)
	}
}

type catalogFactsOnlyTablePlugin struct {
	engineType string
	facts      *plugin.EngineCatalogFacts
}

type catalogFactsOnlyDirectLeafPlugin struct {
	engineType string
	facts      *plugin.EngineCatalogFacts
	paths      []plugin.EngineCatalogPath
}

func (p *catalogFactsOnlyDirectLeafPlugin) Type() string { return p.engineType }
func (p *catalogFactsOnlyDirectLeafPlugin) DisplayName() string {
	return "known refresh topic test"
}
func (p *catalogFactsOnlyDirectLeafPlugin) EngineOrigin() string { return "general" }
func (p *catalogFactsOnlyDirectLeafPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *catalogFactsOnlyDirectLeafPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *catalogFactsOnlyDirectLeafPlugin) DefaultPort() int          { return 0 }
func (p *catalogFactsOnlyDirectLeafPlugin) RequiredFields() []string  { return nil }
func (p *catalogFactsOnlyDirectLeafPlugin) SensitiveFields() []string { return nil }
func (p *catalogFactsOnlyDirectLeafPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{SchemaVersion: plugin.CapabilitiesSchemaVersion, EngineType: p.Type()}
}
func (p *catalogFactsOnlyDirectLeafPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.EngineCatalogModelSpec{
		PathVersion: plugin.EngineCatalogPathVersion,
		RootTerm:    plugin.EngineCatalogTermService,
		Levels: []plugin.EngineCatalogLevelSpec{{
			Term: "topic", Kinds: []string{"topic"}, Role: plugin.EngineCatalogRoleLeaf,
		}},
	}
}
func (p *catalogFactsOnlyDirectLeafPlugin) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.paths = append(p.paths, path)
	return p.facts, nil
}

func (p *catalogFactsOnlyTablePlugin) Type() string { return p.engineType }
func (p *catalogFactsOnlyTablePlugin) DisplayName() string {
	return "known refresh table test"
}
func (p *catalogFactsOnlyTablePlugin) EngineOrigin() string { return "general" }
func (p *catalogFactsOnlyTablePlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *catalogFactsOnlyTablePlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p *catalogFactsOnlyTablePlugin) DefaultPort() int                                   { return 0 }
func (p *catalogFactsOnlyTablePlugin) RequiredFields() []string                           { return nil }
func (p *catalogFactsOnlyTablePlugin) SensitiveFields() []string                          { return nil }
func (p *catalogFactsOnlyTablePlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), plugin.EngineCatalogTermSchema, plugin.TabularCapabilityOptions{SpatialFacts: true})
}
func (p *catalogFactsOnlyTablePlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
}
func (p *catalogFactsOnlyTablePlugin) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return p.facts, nil
}

type knownRefreshOSGBContentReader struct {
	staticObjectContentReader
}

type knownRefreshDynamicSchemaPlugin struct {
	facts          *plugin.EngineCatalogFacts
	sampledPath    plugin.EngineCatalogPath
	sampledOptions plugin.EngineCatalogFactsOptions
}

func (p *knownRefreshDynamicSchemaPlugin) Type() string { return "known-refresh-dynamic-schema-test" }
func (p *knownRefreshDynamicSchemaPlugin) DisplayName() string {
	return "known refresh dynamic schema test"
}
func (p *knownRefreshDynamicSchemaPlugin) EngineOrigin() string { return "general" }
func (p *knownRefreshDynamicSchemaPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *knownRefreshDynamicSchemaPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *knownRefreshDynamicSchemaPlugin) DefaultPort() int          { return 0 }
func (p *knownRefreshDynamicSchemaPlugin) RequiredFields() []string  { return nil }
func (p *knownRefreshDynamicSchemaPlugin) SensitiveFields() []string { return nil }
func (p *knownRefreshDynamicSchemaPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewDynamicSchemaCapabilities(p.Type())
}
func (p *knownRefreshDynamicSchemaPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.DynamicSchemaCatalogModel()
}
func (p *knownRefreshDynamicSchemaPlugin) SampleDynamicSchema(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.sampledPath = path
	p.sampledOptions = opts
	return p.facts, nil
}

func (r knownRefreshOSGBContentReader) Type() string { return "known-refresh-osgb-test" }

type knownRefreshOSGBSceneProvider struct {
	contents map[string]string
}

func (p knownRefreshOSGBSceneProvider) Type() string         { return "known-refresh-osgb-scene-test" }
func (p knownRefreshOSGBSceneProvider) DisplayName() string  { return "known refresh osgb scene test" }
func (p knownRefreshOSGBSceneProvider) EngineOrigin() string { return "general" }
func (p knownRefreshOSGBSceneProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p knownRefreshOSGBSceneProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p knownRefreshOSGBSceneProvider) DefaultPort() int          { return 0 }
func (p knownRefreshOSGBSceneProvider) RequiredFields() []string  { return nil }
func (p knownRefreshOSGBSceneProvider) SensitiveFields() []string { return nil }
func (p knownRefreshOSGBSceneProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.NewFileCapabilities(p.Type())
}
func (p knownRefreshOSGBSceneProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p knownRefreshOSGBSceneProvider) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.FileCatalogModel()
}
func (p knownRefreshOSGBSceneProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	scopePath := strings.Trim(parent.StringPath(), "/")
	metadataPath := scopePath + "/metadata.xml"
	dataPath := scopePath + "/Data"
	tileDirPath := dataPath + "/Tile_1"
	tilePath := tileDirPath + "/Tile_1_L14_0.osgb"
	entries := []plugin.EngineCatalogEntry{
		knownRefreshFileEntry(1, "metadata.xml", metadataPath, int64(len(p.contents[metadataPath]))),
		knownRefreshDirEntry(1, "Data", dataPath),
	}
	if opts.Recursive {
		entries = append(entries,
			knownRefreshDirEntry(1, "Tile_1", tileDirPath),
			knownRefreshFileEntry(1, "Tile_1_L14_0.osgb", tilePath, int64(len(p.contents[tilePath]))),
		)
	}
	return entries, nil
}
func (p knownRefreshOSGBSceneProvider) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return nil, nil
}
func (p knownRefreshOSGBSceneProvider) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	content, ok := p.contents[strings.Trim(path.StringPath(), "/")]
	if !ok {
		return nil, fmt.Errorf("content not found: %s", path.StringPath())
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func knownRefreshDirEntry(engineID uint, name, path string) plugin.EngineCatalogEntry {
	return plugin.EngineCatalogEntry{
		Name: name,
		Path: plugin.FileDirectoryPath(engineID, path),
		Term: plugin.EngineCatalogTermDirectory,
		Kind: plugin.EngineCatalogKindDirectory,
		Role: plugin.EngineCatalogRoleBranch,
		Storage: &plugin.EngineCatalogStorageFacts{
			Path: path,
		},
	}
}

func knownRefreshFileEntry(engineID uint, name, path string, size int64) plugin.EngineCatalogEntry {
	return plugin.EngineCatalogEntry{
		Name: name,
		Path: plugin.FileItemPath(engineID, path),
		Term: plugin.EngineCatalogTermFile,
		Kind: plugin.EngineCatalogKindFile,
		Role: plugin.EngineCatalogRoleLeaf,
		Storage: &plugin.EngineCatalogStorageFacts{
			Path:      path,
			SizeBytes: &size,
		},
	}
}

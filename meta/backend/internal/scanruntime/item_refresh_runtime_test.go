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
		engineType: "known-refresh-table-test",
		facts: &plugin.CatalogFacts{
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
					Source:             datatype.CRSDefinitionSourcePostGISSpatialRefSys,
				}},
			},
		},
	}
	pluginRegisterForTest(t, enginePlugin)

	repo := metaRepo.NewScanRepository(db)
	parentNode := models.MetaNode{
		TenantID:   tenantID,
		EngineID:   engineID,
		NodeType:   plugin.CatalogTermSchema,
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
		ItemType:    plugin.CatalogTermTable,
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
	tableInfo := datatype.TableInfoFromPayload(commonJSON.Section(refreshed.Attributes, "type_info.table"), "roads")
	if tableInfo == nil || len(tableInfo.Fields) != 2 {
		t.Fatalf("type_info.table = %#v", commonJSON.Section(refreshed.Attributes, "type_info.table"))
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
		NodeType:   plugin.CatalogTermDirectory,
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
		ItemType:    plugin.CatalogTermFile,
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
		NodeType:   plugin.CatalogTermDirectory,
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
		ItemType:    plugin.CatalogTermFile,
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
	facts      *plugin.CatalogFacts
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
	return plugin.NewTabularCapabilities(p.Type(), plugin.CatalogTermSchema, plugin.TabularCapabilityOptions{SpatialFacts: true})
}
func (p *catalogFactsOnlyTablePlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.CatalogTermSchema)
}
func (p *catalogFactsOnlyTablePlugin) DescribeCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return p.facts, nil
}

type knownRefreshOSGBContentReader struct {
	staticObjectContentReader
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
func (p knownRefreshOSGBSceneProvider) CatalogModel() plugin.CatalogModelSpec {
	return plugin.FileCatalogModel()
}
func (p knownRefreshOSGBSceneProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	scopePath := strings.Trim(parent.StringPath(), "/")
	metadataPath := scopePath + "/metadata.xml"
	dataPath := scopePath + "/Data"
	tileDirPath := dataPath + "/Tile_1"
	tilePath := tileDirPath + "/Tile_1_L14_0.osgb"
	entries := []plugin.CatalogEntry{
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
func (p knownRefreshOSGBSceneProvider) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return nil, nil
}
func (p knownRefreshOSGBSceneProvider) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	content, ok := p.contents[strings.Trim(path.StringPath(), "/")]
	if !ok {
		return nil, fmt.Errorf("content not found: %s", path.StringPath())
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func knownRefreshDirEntry(engineID uint, name, path string) plugin.CatalogEntry {
	return plugin.CatalogEntry{
		Name: name,
		Path: plugin.FileDirectoryPath(engineID, path),
		Term: plugin.CatalogTermDirectory,
		Kind: plugin.CatalogKindDirectory,
		Role: plugin.CatalogRoleBranch,
		Storage: &plugin.CatalogStorageFacts{
			Path: path,
		},
	}
}

func knownRefreshFileEntry(engineID uint, name, path string, size int64) plugin.CatalogEntry {
	return plugin.CatalogEntry{
		Name: name,
		Path: plugin.FileItemPath(engineID, path),
		Term: plugin.CatalogTermFile,
		Kind: plugin.CatalogKindFile,
		Role: plugin.CatalogRoleLeaf,
		Storage: &plugin.CatalogStorageFacts{
			Path:      path,
			SizeBytes: &size,
		},
	}
}

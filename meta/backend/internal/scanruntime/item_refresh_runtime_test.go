package scanruntime

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
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
		ID:         engineID,
		TenantID:   &tenantID,
		EngineType: enginePlugin.Type(),
		IsActive:   true,
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

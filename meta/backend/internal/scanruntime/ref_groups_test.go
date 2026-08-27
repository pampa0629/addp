package scanruntime

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func TestObjectScanRefGroupsPersistsSingleShapefileItem(t *testing.T) {
	provider := &objectRefGroupScanTestProvider{content: "", buckets: []string{"manager"}}
	pluginRegisterForTest(t, provider)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 9, Name: "Object Store", EngineType: provider.Type()}

	result, err := svc.ScanRefGroups(context.Background(), resource, 1, []models.ScanRefGroup{
		{
			Primary: "manager/a5.shp",
			Refs: []models.ScanRef{
				{Path: "manager/a5.shp", Role: "main", Required: true},
				{Path: "manager/a5.shx", Role: "sidecar", Required: true},
				{Path: "manager/a5.dbf", Role: "sidecar", Required: true},
				{Path: "manager/a5.cpg", Role: "sidecar"},
			},
		},
	}, models.ScannedDepthDeep, true, nil)
	if err != nil {
		t.Fatalf("ScanRefGroups() error = %v", err)
	}
	if result.Items != 1 {
		t.Fatalf("items = %d, want one logical shapefile item", result.Items)
	}

	item, ok, err := repo.FindItemByFullName(1, 9, "manager/a5.shp")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("shapefile item not found")
	}
	assertShapefileLogicalItem(t, item.Attributes, []string{
		"manager/a5.shp",
		"manager/a5.shx",
		"manager/a5.dbf",
		"manager/a5.cpg",
	}, []string{
		"manager/a5.prj",
		"manager/a5.qpj",
		"manager/a5.sbn",
		"manager/a5.sbx",
	})
}

func TestObjectScanRefGroupsPersistsSingleUploadedObjectItem(t *testing.T) {
	provider := &objectRefGroupScanTestProvider{content: "hello", buckets: []string{"manager"}}
	pluginRegisterForTest(t, provider)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 19, Name: "Object Store", EngineType: provider.Type()}

	result, err := svc.ScanRefGroups(context.Background(), resource, 1, []models.ScanRefGroup{
		{
			Primary: "manager/ZX书单.rtf",
			Refs: []models.ScanRef{
				{Path: "manager/ZX书单.rtf", Role: "main", Required: true},
			},
		},
	}, models.ScannedDepthDeep, true, nil)
	if err != nil {
		t.Fatalf("ScanRefGroups() error = %v", err)
	}
	if result.Items != 1 {
		t.Fatalf("items = %d, want one uploaded object item", result.Items)
	}

	item, ok, err := repo.FindItemByFullName(1, 19, "manager/ZX书单.rtf")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("uploaded object item not found")
	}
	storage, ok := item.Attributes["storage"].(map[string]interface{})
	if !ok {
		t.Fatalf("storage attributes = %#v", item.Attributes["storage"])
	}
	if storage["bucket"] != "manager" || storage["name"] != "ZX书单.rtf" {
		t.Fatalf("storage attributes = %#v, want bucket manager and object name", storage)
	}
}

func TestObjectScanRefGroupsDeepScansSingleGeoJSONWithoutRepeatingBucket(t *testing.T) {
	provider := &objectRefGroupScanTestProvider{content: `{
		"type": "FeatureCollection",
		"bbox": [1, 2, 3, 4],
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A"}}
		]
	}`, buckets: []string{"manager"}}
	pluginRegisterForTest(t, provider)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 29, Name: "Object Store", EngineType: provider.Type()}

	result, err := svc.ScanRefGroups(context.Background(), resource, 1, []models.ScanRefGroup{
		{
			Primary: "manager/farmland.geojson",
			Refs: []models.ScanRef{
				{Path: "manager/farmland.geojson", Role: "main", Required: true},
			},
		},
	}, models.ScannedDepthDeep, true, nil)
	if err != nil {
		t.Fatalf("ScanRefGroups() error = %v", err)
	}
	if result.Items != 1 {
		t.Fatalf("items = %d, want one GeoJSON item", result.Items)
	}

	for _, got := range provider.openedPaths {
		if got == "manager/manager/farmland.geojson" {
			t.Fatalf("OpenContent used repeated bucket path: %#v", provider.openedPaths)
		}
	}
	if !containsString(provider.openedPaths, "manager/farmland.geojson") {
		t.Fatalf("OpenContent paths = %#v, want manager/farmland.geojson", provider.openedPaths)
	}

	item, ok, err := repo.FindItemByFullName(1, 29, "manager/farmland.geojson")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("geojson item not found")
	}
	if got := commonJSON.String(item.Attributes, "item", "format"); got != "geojson" {
		t.Fatalf("item.format = %q, want geojson", got)
	}
	if table := datatype.TableInfoFromPayload(commonJSON.Section(item.Attributes, "type_info.table"), ""); table == nil || table.GetField("geometry") == nil {
		t.Fatalf("type_info.table = %#v, want geometry field", commonJSON.Section(item.Attributes, "type_info.table"))
	}
	spatial := commonJSON.Section(item.Attributes, "capabilities.spatial")
	if spatial["primary_geometry_column"] != "geometry" {
		t.Fatalf("capabilities.spatial = %#v, want geometry primary column", spatial)
	}
}

func TestObjectScanRefGroupsRejectsUnknownBucketWithoutPersistingItem(t *testing.T) {
	provider := &objectRefGroupScanTestProvider{content: "", buckets: []string{"addp"}}
	pluginRegisterForTest(t, provider)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 39, Name: "Object Store", EngineType: provider.Type()}

	_, err := svc.ScanRefGroups(context.Background(), resource, 1, []models.ScanRefGroup{
		{
			Primary: "gis/a2.shp",
			Refs: []models.ScanRef{
				{Path: "gis/a2.shp", Role: "main", Required: true},
				{Path: "gis/a2.shx", Role: "sidecar", Required: true},
				{Path: "gis/a2.dbf", Role: "sidecar", Required: true},
				{Path: "gis/a2.prj", Role: "sidecar"},
			},
		},
	}, models.ScannedDepthDeep, true, nil)
	if err == nil {
		t.Fatal("ScanRefGroups() error = nil, want unknown bucket error")
	}
	if !strings.Contains(err.Error(), `object ref group bucket "gis" does not exist`) {
		t.Fatalf("ScanRefGroups() error = %v, want unknown bucket error", err)
	}

	if _, ok, err := repo.FindItemByFullName(1, 39, "gis/a2.shp"); err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	} else if ok {
		t.Fatal("unexpected item persisted for unknown bucket")
	}
	var bucketNodeCount int64
	if err := db.Model(&models.MetaNode{}).Where("tenant_id = ? AND engine_id = ? AND node_type = ? AND full_name = ? AND deleted_at IS NULL", 1, 39, "bucket", "gis").Count(&bucketNodeCount).Error; err != nil {
		t.Fatalf("count bucket node error = %v", err)
	}
	if bucketNodeCount != 0 {
		t.Fatalf("unknown bucket nodes = %d, want 0", bucketNodeCount)
	}
}

func TestFileScanRefGroupsPersistsSingleShapefileItem(t *testing.T) {
	provider := filesystemScanTestProvider{content: ""}
	pluginRegisterForTest(t, provider)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 26, Name: "Files", EngineType: provider.Type()}

	result, err := svc.ScanRefGroups(context.Background(), resource, 1, []models.ScanRefGroup{
		{
			Primary: "shp/a5.shp",
			Refs: []models.ScanRef{
				{Path: "shp/a5.shp", Role: "main", Required: true},
				{Path: "shp/a5.shx", Role: "sidecar", Required: true},
				{Path: "shp/a5.dbf", Role: "sidecar", Required: true},
				{Path: "shp/a5.cpg", Role: "sidecar"},
			},
		},
	}, models.ScannedDepthDeep, true, nil)
	if err != nil {
		t.Fatalf("ScanRefGroups() error = %v", err)
	}
	if result.Items != 1 {
		t.Fatalf("items = %d, want one logical shapefile item", result.Items)
	}

	item, ok, err := repo.FindItemByFullName(1, 26, "shp/a5.shp")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("shapefile item not found")
	}
	assertShapefileLogicalItem(t, item.Attributes, []string{
		"shp/a5.shp",
		"shp/a5.shx",
		"shp/a5.dbf",
		"shp/a5.cpg",
	}, []string{
		"shp/a5.prj",
		"shp/a5.qpj",
		"shp/a5.sbn",
		"shp/a5.sbx",
	})
}

func TestFileScanRefGroupsPersistsFileGDBScopeItem(t *testing.T) {
	sizeBytes := int64(12)
	provider := filesystemScanTestProvider{
		entriesByPath: map[string][]plugin.EngineCatalogEntry{
			"arcgis/pgeo_roundtrip.gdb": {
				{
					Name: "a00000001.gdbtable",
					Path: plugin.FileItemPath(27, "arcgis/pgeo_roundtrip.gdb/a00000001.gdbtable"),
					Term: plugin.EngineCatalogTermFile,
					Kind: plugin.EngineCatalogKindFile,
					Role: plugin.EngineCatalogRoleLeaf,
					Storage: &plugin.EngineCatalogStorageFacts{
						Path:      "arcgis/pgeo_roundtrip.gdb/a00000001.gdbtable",
						SizeBytes: &sizeBytes,
					},
				},
				{
					Name: "a00000001.gdbtablx",
					Path: plugin.FileItemPath(27, "arcgis/pgeo_roundtrip.gdb/a00000001.gdbtablx"),
					Term: plugin.EngineCatalogTermFile,
					Kind: plugin.EngineCatalogKindFile,
					Role: plugin.EngineCatalogRoleLeaf,
					Storage: &plugin.EngineCatalogStorageFacts{
						Path:      "arcgis/pgeo_roundtrip.gdb/a00000001.gdbtablx",
						SizeBytes: &sizeBytes,
					},
				},
			},
		},
	}
	pluginRegisterForTest(t, provider)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 27, Name: "Files", EngineType: provider.Type()}

	result, err := svc.ScanRefGroups(context.Background(), resource, 1, []models.ScanRefGroup{
		{
			Primary: "arcgis/pgeo_roundtrip.gdb",
			Refs: []models.ScanRef{
				{Path: "arcgis/pgeo_roundtrip.gdb", Role: "scope", Required: true},
			},
		},
	}, models.ScannedDepthBasic, true, nil)
	if err != nil {
		t.Fatalf("ScanRefGroups() error = %v", err)
	}
	if result.Items != 1 {
		t.Fatalf("items = %d, want one FileGDB item", result.Items)
	}

	item, ok, err := repo.FindItemByFullName(1, 27, "arcgis/pgeo_roundtrip.gdb")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("FileGDB item not found")
	}
	if got := commonJSON.String(item.Attributes, "item", "layout"); got != "whole" {
		t.Fatalf("item.layout = %q, want whole", got)
	}
	if got := commonJSON.String(item.Attributes, "item", "data_type"); got != "container" {
		t.Fatalf("item.data_type = %q, want container", got)
	}
	if got := commonJSON.String(item.Attributes, "item", "format"); got != "filegdb" {
		t.Fatalf("item.format = %q, want filegdb", got)
	}
	if got := commonJSON.String(item.Attributes, "item", "claim_policy"); got != "whole_scope" {
		t.Fatalf("item.claim_policy = %q, want whole_scope", got)
	}
}

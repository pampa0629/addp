package scanruntime

import (
	"context"
	"io"
	"log/slog"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

func TestDetectObjectCatalogResourceFormatUsesCommonFormatSniffing(t *testing.T) {
	t.Parallel()

	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "datasets/lake3",
		FullPath:    "addp/datasets/lake3",
		NodeType:    "object",
		SizeBytes:   8,
		CatalogPath: plugin.ObjectItemPath(7, "addp", "datasets/lake3"),
	}

	detected, err := detectObjectCatalogResourceFormat(
		context.Background(),
		staticObjectContentReader{content: "PAR1\x15\x04\x15\x00"},
		nil,
		resource,
	)
	if err != nil {
		t.Fatalf("detectObjectCatalogResourceFormat() error = %v", err)
	}
	if detected != string(format.FormatParquet) {
		t.Fatalf("detected format = %q, want parquet", detected)
	}
}

func TestObjectCatalogEntriesToStorageResourcesIgnoresSystemFiles(t *testing.T) {
	t.Parallel()

	readmeSize := int64(12)
	systemSize := int64(1)
	entries := []plugin.CatalogEntry{
		{
			Name: ".DS_Store",
			Path: plugin.ObjectItemPath(7, "addp", "docs/.DS_Store"),
			Role: plugin.CatalogRoleLeaf,
			Kind: plugin.CatalogKindObject,
			Storage: &plugin.CatalogStorageFacts{
				Path:      "addp/docs/.DS_Store",
				SizeBytes: &systemSize,
			},
		},
		{
			Name: "README.md",
			Path: plugin.ObjectItemPath(7, "addp", "docs/README.md"),
			Role: plugin.CatalogRoleLeaf,
			Kind: plugin.CatalogKindObject,
			Storage: &plugin.CatalogStorageFacts{
				Path:      "addp/docs/README.md",
				SizeBytes: &readmeSize,
			},
		},
	}

	resources := objectCatalogEntriesToStorageResources(entries, "addp")
	if len(resources) != 1 || resources[0].FullPath != "addp/docs/README.md" {
		t.Fatalf("resources = %#v, want only README.md", resources)
	}
}

func TestDetectObjectCatalogResourceFormatPromotesUnknownText(t *testing.T) {
	t.Parallel()

	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "docs/README",
		FullPath:    "addp/docs/README",
		NodeType:    "object",
		SizeBytes:   12,
		CatalogPath: plugin.ObjectItemPath(7, "addp", "docs/README"),
	}

	detected, err := detectObjectCatalogResourceFormat(
		context.Background(),
		staticObjectContentReader{content: "hello\nworld\n"},
		nil,
		resource,
	)
	if err != nil {
		t.Fatalf("detectObjectCatalogResourceFormat() error = %v", err)
	}
	if detected != string(format.FormatText) {
		t.Fatalf("detected format = %q, want text", detected)
	}
}

func TestDetectObjectCatalogResourceFormatKeepsUnknownBinary(t *testing.T) {
	t.Parallel()

	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "docs/blob.binx",
		FullPath:    "addp/docs/blob.binx",
		NodeType:    "object",
		SizeBytes:   3,
		CatalogPath: plugin.ObjectItemPath(7, "addp", "docs/blob.binx"),
	}

	detected, err := detectObjectCatalogResourceFormat(
		context.Background(),
		staticObjectContentReader{content: string([]byte{0x00, 0x01, 0x02})},
		nil,
		resource,
	)
	if err != nil {
		t.Fatalf("detectObjectCatalogResourceFormat() error = %v", err)
	}
	if detected != "" {
		t.Fatalf("detected format = %q, want empty unknown", detected)
	}
}

func TestEnsureObjectCatalogPrefixNodesUsesCompositeItemParentPath(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)

	bucketNode, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	stats := map[uint]*scanflow.ObjectCatalogNodeAggregate{}
	parentNode, err := runtime.ensureObjectCatalogPrefixNodes(1, 9, bucketNode, bucketNode, "gis/", "", stats)
	if err != nil {
		t.Fatalf("ensure prefix nodes: %v", err)
	}

	if parentNode.ID == bucketNode.ID {
		t.Fatal("composite item under addp/gis should attach to gis prefix, not bucket scope")
	}
	if parentNode.NodeType != "prefix" || parentNode.Name != "gis" || parentNode.FullName != "addp/gis" {
		t.Fatalf("parent node = %#v, want addp/gis prefix", parentNode)
	}
	if _, ok := stats[parentNode.ID]; !ok {
		t.Fatalf("gis prefix aggregate was not initialized")
	}
}

func TestObjectCatalogBasicScanGroupsShapefileRefsWithoutSidecarItems(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	reader := staticObjectContentReader{content: ""}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)

	bucketNode, err := repo.UpsertNode(1, 9, nil, "bucket", "manager", strPtr("manager"), metacatalog.ObjectBucketNodeAttributes("manager"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	resources := []metacatalog.StorageResource{
		shapefileObjectResource(9, "manager", "a5.shp", 10),
		shapefileObjectResource(9, "manager", "a5.shx", 11),
		shapefileObjectResource(9, "manager", "a5.dbf", 12),
		shapefileObjectResource(9, "manager", "a5.cpg", 3),
	}
	count, _, err := runtime.persistObjectResources(context.Background(),
		&commonModels.Engine{ID: 9, EngineType: reader.Type()},
		1,
		9,
		bucketNode,
		resources,
		map[uint]*scanflow.ObjectCatalogNodeAggregate{},
		true,
		models.ScannedDepthBasic,
		true,
		"",
		map[string]bool{},
		"object",
	)
	if err != nil {
		t.Fatalf("persistObjectResources() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted count = %d, want one logical shapefile item", count)
	}

	var items []models.MetaItem
	if err := db.Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL", 1, 9).Order("full_name").Find(&items).Error; err != nil {
		t.Fatalf("query items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want only shapefile logical item", items)
	}
	item := items[0]
	if item.FullName != "manager/a5.shp" || item.Name != "a5.shp" {
		t.Fatalf("item identity = %s/%s, want manager/a5.shp", item.FullName, item.Name)
	}
	assertShapefileLogicalItem(t, item.Attributes, []string{
		"manager/a5.shp",
		"manager/a5.shx",
		"manager/a5.dbf",
		"manager/a5.cpg",
	}, nil)
}

func TestObjectCatalogBasicScanGroupsGeoTIFFRefsWithoutSidecarItems(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	reader := staticObjectContentReader{content: ""}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)

	bucketNode, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	resources := []metacatalog.StorageResource{
		geotiffObjectResource(9, "addp", "image/srtm_40_01.tif", 100),
		geotiffObjectResource(9, "addp", "image/srtm_40_01.tfw", 10),
		geotiffObjectResource(9, "addp", "image/srtm_40_01.hdr", 20),
		geotiffObjectResource(9, "addp", "image/srtm_40_01.tif.aux.xml", 30),
	}
	stats := map[uint]*scanflow.ObjectCatalogNodeAggregate{}
	count, _, err := runtime.persistObjectResources(context.Background(),
		&commonModels.Engine{ID: 9, EngineType: reader.Type()},
		1,
		9,
		bucketNode,
		resources,
		stats,
		true,
		models.ScannedDepthBasic,
		true,
		"",
		map[string]bool{},
		"object",
	)
	if err != nil {
		t.Fatalf("persistObjectResources() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted count = %d, want one logical GeoTIFF item", count)
	}
	if agg := stats[bucketNode.ID]; agg == nil || agg.ItemCount != 1 {
		t.Fatalf("bucket aggregate = %#v, want one logical GeoTIFF item", agg)
	}

	var items []models.MetaItem
	if err := db.Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL", 1, 9).Order("full_name").Find(&items).Error; err != nil {
		t.Fatalf("query items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want only GeoTIFF logical item", items)
	}
	item := items[0]
	if item.FullName != "addp/image/srtm_40_01.tif" || item.Name != "srtm_40_01.tif" {
		t.Fatalf("item identity = %s/%s, want addp/image/srtm_40_01.tif", item.FullName, item.Name)
	}
	assertGeoTIFFLogicalItem(t, item.Attributes, []string{
		"addp/image/srtm_40_01.tif",
		"addp/image/srtm_40_01.tfw",
		"addp/image/srtm_40_01.hdr",
		"addp/image/srtm_40_01.tif.aux.xml",
	})
}

func TestObjectCatalogDeepScanDetectsRasterMosaicDatasetItem(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	reader := staticObjectContentReader{content: `{
		"schema_version":"addp.raster_mosaic.v1",
		"data_type":"media",
		"format":"raster_mosaic",
		"layout":"whole",
		"dataset_name":"srtm-test",
		"refs":{"index":"index/source-index.json","overview":"overviews/overview.cog.tif"},
		"summary":{
			"leaf_count":2,
			"source_count":2,
			"failed_count":0,
			"extent":[15,15,155,60],
			"source_crs":"EPSG:4326",
			"overview_width":14110,
			"overview_height":4535
		},
		"capabilities":{"leaf_cog":true,"global_overview_cog":true,"backend_tile_preview":true}
	}`}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)

	bucketNode, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	resources := []metacatalog.StorageResource{
		objectResourceForTest(9, "addp", "mosaics/srtm-test/srtm-test/mosaic.addp.json", 1200, "json"),
		objectResourceForTest(9, "addp", "mosaics/srtm-test/srtm-test/index/source-index.json", 2400, "json"),
		geotiffObjectResource(9, "addp", "mosaics/srtm-test/srtm-test/overviews/overview.cog.tif", 3000),
		geotiffObjectResource(9, "addp", "mosaics/srtm-test/srtm-test/leaf/srtm_40_01.cog.tif", 4000),
		geotiffObjectResource(9, "addp", "mosaics/srtm-test/srtm-test/leaf/srtm_46_02.cog.tif", 5000),
	}
	stats := map[uint]*scanflow.ObjectCatalogNodeAggregate{}
	count, _, err := runtime.persistObjectResources(context.Background(),
		&commonModels.Engine{ID: 9, EngineType: reader.Type()},
		1,
		9,
		bucketNode,
		resources,
		stats,
		true,
		models.ScannedDepthDeep,
		true,
		"",
		map[string]bool{},
		"object",
	)
	if err != nil {
		t.Fatalf("persistObjectResources() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted count = %d, want one raster mosaic dataset item", count)
	}

	var items []models.MetaItem
	if err := db.Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL", 1, 9).Order("full_name").Find(&items).Error; err != nil {
		t.Fatalf("query items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want only raster mosaic dataset item", items)
	}
	var nodes []models.MetaNode
	if err := db.Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL", 1, 9).Order("full_name").Find(&nodes).Error; err != nil {
		t.Fatalf("query nodes: %v", err)
	}
	nodeNames := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.FullName)
	}
	wantNodes := []string{
		"addp",
		"addp/mosaics",
		"addp/mosaics/srtm-test",
	}
	if !reflect.DeepEqual(nodeNames, wantNodes) {
		t.Fatalf("nodes = %#v, want only mosaic parent path nodes %#v", nodeNames, wantNodes)
	}
	item := items[0]
	if item.FullName != "addp/mosaics/srtm-test/srtm-test" || item.Name != "srtm-test" {
		t.Fatalf("item identity = %s/%s, want addp/mosaics/srtm-test/srtm-test", item.FullName, item.Name)
	}
	if got := jsonStringAt(item.Attributes, "item", "format"); got != "raster_mosaic" {
		t.Fatalf("item.format = %q, want raster_mosaic", got)
	}
	if got := jsonStringAt(item.Attributes, "item", "layout"); got != "whole" {
		t.Fatalf("item.layout = %q, want whole", got)
	}
	info := jsonMapAt(item.Attributes, "format_info", "raster_mosaic")
	if info["overview_ref"] != "overviews/overview.cog.tif" || info["index_ref"] != "index/source-index.json" {
		t.Fatalf("raster_mosaic format_info = %#v", info)
	}
	spatial := jsonMapAt(item.Attributes, "capabilities", "spatial")
	if commonJSON.InterfaceInt64(spatial["srid"]) != 4326 {
		t.Fatalf("spatial = %#v, want srid 4326", spatial)
	}
}

func TestObjectCatalogDeepScanDetectsGLBModel3DItem(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	content := scanRuntimeTestGLB([]byte(`{
		"asset":{"version":"2.0","generator":"scanruntime-test"},
		"nodes":[{}],
		"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1}]}],
		"accessors":[
			{"count":8,"type":"VEC3","min":[1,2,3],"max":[4,5,6]},
			{"count":12,"type":"SCALAR"}
		]
	}`))
	reader := staticObjectContentReader{content: string(content)}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)

	bucketNode, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	resources := []metacatalog.StorageResource{
		objectResourceForTest(9, "addp", "models/building.glb", int64(len(content)), ""),
	}
	count, _, err := runtime.persistObjectResources(context.Background(),
		&commonModels.Engine{ID: 9, EngineType: reader.Type()},
		1,
		9,
		bucketNode,
		resources,
		map[uint]*scanflow.ObjectCatalogNodeAggregate{},
		true,
		models.ScannedDepthDeep,
		true,
		"",
		map[string]bool{},
		"object",
	)
	if err != nil {
		t.Fatalf("persistObjectResources() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted count = %d, want one GLB item", count)
	}

	item, ok, err := repo.FindItemByFullName(1, 9, "addp/models/building.glb")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("GLB item not found")
	}
	if got := commonJSON.String(item.Attributes, "item", "data_type"); got != string(datatype.Model3D) {
		t.Fatalf("item.data_type = %q, want model_3d", got)
	}
	if got := commonJSON.String(item.Attributes, "item", "format"); got != string(format.FormatGLB) {
		t.Fatalf("item.format = %q, want glb", got)
	}
	model := commonJSON.Section(item.Attributes, "type_info.model_3d")
	if model["model_kind"] != string(datatype.Model3DKindMeshScene) || commonJSON.InterfaceInt64(model["vertex_count"]) != 8 {
		t.Fatalf("type_info.model_3d = %#v, want mesh_scene with vertex_count 8", model)
	}
	formatInfo := commonJSON.Section(item.Attributes, "format_info.glb")
	if formatInfo["gltf_version"] != "2.0" {
		t.Fatalf("format_info.glb = %#v, want gltf_version 2.0", formatInfo)
	}
}

func TestObjectCatalogDeepScanDetects3DTilesModel3DItem(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	reader := staticObjectContentReader{content: `{
		"asset":{"version":"1.1"},
		"geometricError":200,
		"root":{
			"boundingVolume":{"region":[1,0.5,1.1,0.6,0,120]},
			"geometricError":0,
			"content":{"uri":"root.b3dm"}
		}
	}`}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)

	bucketNode, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	resources := []metacatalog.StorageResource{
		objectResourceForTest(9, "addp", "models/city/tileset.json", 1200, ""),
		objectResourceForTest(9, "addp", "models/city/root.b3dm", 2400, ""),
	}
	count, _, err := runtime.persistObjectResources(context.Background(),
		&commonModels.Engine{ID: 9, EngineType: reader.Type()},
		1,
		9,
		bucketNode,
		resources,
		map[uint]*scanflow.ObjectCatalogNodeAggregate{},
		true,
		models.ScannedDepthDeep,
		true,
		"",
		map[string]bool{},
		"object",
	)
	if err != nil {
		t.Fatalf("persistObjectResources() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted count = %d, want one 3D Tiles item", count)
	}

	item, ok, err := repo.FindItemByFullName(1, 9, "addp/models/city")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("3D Tiles item not found")
	}
	if got := commonJSON.String(item.Attributes, "item", "data_type"); got != string(datatype.Model3D) {
		t.Fatalf("item.data_type = %q, want model_3d", got)
	}
	if got := commonJSON.String(item.Attributes, "item", "format"); got != string(format.Format3DTiles) {
		t.Fatalf("item.format = %q, want 3dtiles", got)
	}
	if got := commonJSON.String(item.Attributes, "item", "layout"); got != string(format.LayoutWhole) {
		t.Fatalf("item.layout = %q, want whole", got)
	}
	model := commonJSON.Section(item.Attributes, "type_info.model_3d")
	if model["model_kind"] != string(datatype.Model3DKindTiledScene) {
		t.Fatalf("type_info.model_3d = %#v, want tiled_scene", model)
	}
	formatInfo := commonJSON.Section(item.Attributes, "format_info.3dtiles")
	if formatInfo["asset_version"] != "1.1" || commonJSON.InterfaceInt64(formatInfo["tile_count"]) != 1 {
		t.Fatalf("format_info.3dtiles = %#v, want version and tile_count", formatInfo)
	}
}

func TestObjectCatalogDeepScanDetectsLASPointCloudItem(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	content := scanRuntimeTestLASHeader()
	reader := staticObjectContentReader{content: string(content)}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)

	bucketNode, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	resources := []metacatalog.StorageResource{
		objectResourceForTest(9, "addp", "point-cloud/site.las", int64(len(content)), ""),
	}
	count, _, err := runtime.persistObjectResources(context.Background(),
		&commonModels.Engine{ID: 9, EngineType: reader.Type()},
		1,
		9,
		bucketNode,
		resources,
		map[uint]*scanflow.ObjectCatalogNodeAggregate{},
		true,
		models.ScannedDepthDeep,
		true,
		"",
		map[string]bool{},
		"object",
	)
	if err != nil {
		t.Fatalf("persistObjectResources() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted count = %d, want one LAS item", count)
	}

	item, ok, err := repo.FindItemByFullName(1, 9, "addp/point-cloud/site.las")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("LAS item not found")
	}
	if got := commonJSON.String(item.Attributes, "item", "data_type"); got != string(datatype.PointCloud) {
		t.Fatalf("item.data_type = %q, want point_cloud", got)
	}
	if got := commonJSON.String(item.Attributes, "item", "format"); got != string(format.FormatLAS) {
		t.Fatalf("item.format = %q, want las", got)
	}
	pointCloud := commonJSON.Section(item.Attributes, "type_info.point_cloud")
	if pointCloud["point_cloud_kind"] != string(datatype.PointCloudKindRawPointCloud) || commonJSON.InterfaceInt64(pointCloud["point_count"]) != 123456789 {
		t.Fatalf("type_info.point_cloud = %#v, want raw point cloud with point_count", pointCloud)
	}
	if extent := commonJSON.InterfaceSlice(commonJSON.Section(item.Attributes, "capabilities.spatial")["extent"]); len(extent) != 4 {
		t.Fatalf("capabilities.spatial = %#v, want extent", commonJSON.Section(item.Attributes, "capabilities.spatial"))
	}
	formatInfo := commonJSON.Section(item.Attributes, "format_info.las")
	if formatInfo["version"] != "1.4" {
		t.Fatalf("format_info.las = %#v, want version 1.4", formatInfo)
	}
}

func TestObjectCatalogPrefixScanDeletesLegacyGeoTIFFSidecarItems(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	reader := objectCatalogScanTestProvider{content: ""}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 9, Name: "Object Store", EngineType: reader.Type()}

	rootNode, err := metaRepo.EnsureCatalogRootNode(repo, 1, resource, reader)
	if err != nil {
		t.Fatalf("create root node: %v", err)
	}
	bucketNode, err := repo.UpsertNode(1, 9, rootNode, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}
	imageNode, err := repo.EnsureObjectCatalogPrefixPath(1, 9, bucketNode, "image")
	if err != nil {
		t.Fatalf("create image prefix node: %v", err)
	}
	otherNode, err := repo.EnsureObjectCatalogPrefixPath(1, 9, bucketNode, "other")
	if err != nil {
		t.Fatalf("create other prefix node: %v", err)
	}

	for _, objectPath := range []string{
		"image/srtm_40_01.tif",
		"image/srtm_40_01.tfw",
		"image/srtm_40_01.hdr",
		"image/srtm_40_01.tif.aux.xml",
	} {
		insertObjectItemForTest(t, repo, 1, 9, imageNode, "addp", objectPath, 10)
	}
	insertObjectItemForTest(t, repo, 1, 9, otherNode, "addp", "other/keep.tif", 20)

	result, err := runtime.ScanPaths(context.Background(), resource, 1, []string{"addp/image"}, nil, models.ScannedDepthDeep, true, nil)
	if err != nil {
		t.Fatalf("ScanPaths() error = %v", err)
	}
	if result.Items != 1 {
		t.Fatalf("Items = %d, want one logical GeoTIFF item", result.Items)
	}

	var activeItems []models.MetaItem
	if err := db.Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL", 1, 9).Order("full_name").Find(&activeItems).Error; err != nil {
		t.Fatalf("query active items: %v", err)
	}
	gotNames := make([]string, 0, len(activeItems))
	for _, item := range activeItems {
		gotNames = append(gotNames, item.FullName)
	}
	if !containsString(gotNames, "addp/image/srtm_40_01.tif") || !containsString(gotNames, "addp/other/keep.tif") || len(gotNames) != 2 {
		t.Fatalf("active full names = %#v, want merged image GeoTIFF plus other prefix item", gotNames)
	}

	item, ok, err := repo.FindItemByFullName(1, 9, "addp/image/srtm_40_01.tif")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("merged GeoTIFF item not found")
	}
	assertGeoTIFFLogicalItem(t, item.Attributes, []string{
		"addp/image/srtm_40_01.tif",
		"addp/image/srtm_40_01.tfw",
		"addp/image/srtm_40_01.hdr",
		"addp/image/srtm_40_01.tif.aux.xml",
	})
}

func TestObjectCatalogPrefixScanDeletesStalePrefixConflictingWithWholeItem(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	reader := objectCatalogScanTestProvider{
		contentByPath: map[string]string{
			"addp/mosaics/bigmosaic/bigmosaic/mosaic.addp.json": `{
				"schema_version":"addp.raster_mosaic.v1",
				"data_type":"media",
				"format":"raster_mosaic",
				"layout":"whole",
				"refs":{"index":"index/source-index.json","overview":"overviews/overview.cog.tif"},
				"summary":{"leaf_count":1,"source_count":1,"extent":[82,2,124,28],"source_crs":"EPSG:4326","overview_width":16,"overview_height":8}
			}`,
		},
	}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 9, Name: "Object Store", EngineType: reader.Type()}

	rootNode, err := metaRepo.EnsureCatalogRootNode(repo, 1, resource, reader)
	if err != nil {
		t.Fatalf("create root node: %v", err)
	}
	bucketNode, err := repo.UpsertNode(1, 9, rootNode, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}
	mosaicRoot, err := repo.EnsureObjectCatalogPrefixPath(1, 9, bucketNode, "mosaics/bigmosaic")
	if err != nil {
		t.Fatalf("create mosaic root prefix: %v", err)
	}
	staleWholePathNode, err := repo.EnsureObjectCatalogPrefixPath(1, 9, bucketNode, "mosaics/bigmosaic/bigmosaic")
	if err != nil {
		t.Fatalf("create stale whole item prefix: %v", err)
	}
	staleChild, err := repo.EnsureObjectCatalogPrefixPath(1, 9, bucketNode, "mosaics/bigmosaic/bigmosaic/mosaics/bigmosaic")
	if err != nil {
		t.Fatalf("create stale child prefix: %v", err)
	}
	insertObjectItemForTest(t, repo, 1, 9, staleChild, "addp", "mosaics/bigmosaic/bigmosaic/mosaics/bigmosaic/old.cog.tif", 20)

	resources := []metacatalog.StorageResource{
		objectResourceForTest(9, "addp", "mosaics/bigmosaic/bigmosaic/mosaic.addp.json", 10, "json"),
		objectResourceForTest(9, "addp", "mosaics/bigmosaic/bigmosaic/index/source-index.json", 10, "json"),
		objectResourceForTest(9, "addp", "mosaics/bigmosaic/bigmosaic/overviews/overview.cog.tif", 10, "tiff"),
		objectResourceForTest(9, "addp", "mosaics/bigmosaic/bigmosaic/leaf/a.cog.tif", 10, "tiff"),
	}
	stats := map[uint]*scanflow.ObjectCatalogNodeAggregate{}
	scanned := map[string]bool{}
	count, _, err := runtime.persistObjectResources(context.Background(),
		resource,
		1,
		9,
		bucketNode,
		resources,
		stats,
		false,
		models.ScannedDepthDeep,
		true,
		"mosaics/bigmosaic",
		scanned,
		"object",
	)
	if err != nil {
		t.Fatalf("persistObjectResources() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted count = %d, want one raster mosaic item", count)
	}
	if len(scanned) != 1 {
		t.Fatalf("scanned fingerprints = %d, want one raster mosaic fingerprint", len(scanned))
	}
	if err := repo.HardDeleteInvalidEngineGraph(1, 9); err != nil {
		t.Fatalf("delete stale whole item prefix: %v", err)
	}

	var stale models.MetaNode
	if err := db.Where("id = ?", staleWholePathNode.ID).First(&stale).Error; err == nil {
		t.Fatalf("stale whole item prefix still exists: %#v", stale)
	} else if err != gorm.ErrRecordNotFound {
		t.Fatalf("query stale whole item prefix: %v", err)
	}
	var staleItem models.MetaItem
	if err := db.Where("full_name LIKE ? AND deleted_at IS NULL", "addp/mosaics/bigmosaic/bigmosaic/mosaics/%").First(&staleItem).Error; err == nil {
		t.Fatalf("stale child item still exists: %#v", staleItem)
	} else if err != gorm.ErrRecordNotFound {
		t.Fatalf("query stale child item: %v", err)
	}
	item, ok, err := repo.FindItemByFullName(1, 9, "addp/mosaics/bigmosaic/bigmosaic")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok || item.NodeID != mosaicRoot.ID {
		t.Fatalf("mosaic item = %#v, found=%v, want item under mosaic root %d", item, ok, mosaicRoot.ID)
	}
	if got := commonJSON.Int64(item.Attributes, "capabilities.spatial", "srid"); got != 4326 {
		t.Fatalf("capabilities.spatial.srid = %d, want inferred EPSG:4326", got)
	}
}

func TestObjectCatalogPrefixScanKeepsWholeItemAtScanRoot(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	reader := objectCatalogScanTestProvider{
		contentByPath: map[string]string{
			"addp/mosaics/srtm-e2e/mosaic.addp.json": `{
				"schema_version":"addp.raster_mosaic.v1",
				"data_type":"media",
				"format":"raster_mosaic",
				"layout":"whole",
				"refs":{"index":"index/source-index.json","overview":"overviews/overview.cog.tif"},
				"summary":{"leaf_count":1,"source_count":1,"extent":[15,15,155,60],"source_crs":"EPSG:4326","overview_width":16,"overview_height":8}
			}`,
		},
	}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 9, Name: "Object Store", EngineType: reader.Type()}

	rootNode, err := metaRepo.EnsureCatalogRootNode(repo, 1, resource, reader)
	if err != nil {
		t.Fatalf("create root node: %v", err)
	}
	bucketNode, err := repo.UpsertNode(1, 9, rootNode, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}
	mosaicParent, err := repo.EnsureObjectCatalogPrefixPath(1, 9, bucketNode, "mosaics")
	if err != nil {
		t.Fatalf("create mosaic parent: %v", err)
	}

	resources := []metacatalog.StorageResource{
		objectResourceForTest(9, "addp", "mosaics/srtm-e2e/mosaic.addp.json", 10, "json"),
		objectResourceForTest(9, "addp", "mosaics/srtm-e2e/index/source-index.json", 10, "json"),
		objectResourceForTest(9, "addp", "mosaics/srtm-e2e/overviews/overview.cog.tif", 10, "tiff"),
		objectResourceForTest(9, "addp", "mosaics/srtm-e2e/leaf/a.cog.tif", 10, "tiff"),
	}
	stats := map[uint]*scanflow.ObjectCatalogNodeAggregate{}
	count, _, err := runtime.persistObjectResources(context.Background(),
		resource,
		1,
		9,
		bucketNode,
		resources,
		stats,
		false,
		models.ScannedDepthDeep,
		true,
		"mosaics/srtm-e2e",
		map[string]bool{},
		"object",
	)
	if err != nil {
		t.Fatalf("persistObjectResources() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted count = %d, want one raster mosaic item", count)
	}
	if err := repo.HardDeleteInvalidEngineGraph(1, 9); err != nil {
		t.Fatalf("delete invalid graph: %v", err)
	}

	item, ok, err := repo.FindItemByFullName(1, 9, "addp/mosaics/srtm-e2e")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok || item.NodeID != mosaicParent.ID {
		t.Fatalf("mosaic item = %#v, found=%v, want item under mosaic parent %d", item, ok, mosaicParent.ID)
	}
}

func shapefileObjectResource(engineID uint, bucket, objectPath string, sizeBytes int64) metacatalog.StorageResource {
	return metacatalog.StorageResource{
		RootName:    bucket,
		Path:        objectPath,
		FullPath:    bucket + "/" + objectPath,
		NodeType:    plugin.CatalogKindObject,
		SizeBytes:   sizeBytes,
		Format:      strings.TrimPrefix(strings.ToLower(objectPath[strings.LastIndex(objectPath, "."):]), "."),
		CatalogPath: plugin.ObjectItemPath(engineID, bucket, objectPath),
	}
}

func geotiffObjectResource(engineID uint, bucket, objectPath string, sizeBytes int64) metacatalog.StorageResource {
	return objectResourceForTest(engineID, bucket, objectPath, sizeBytes, "")
}

func objectResourceForTest(engineID uint, bucket, objectPath string, sizeBytes int64, formatName string) metacatalog.StorageResource {
	return metacatalog.StorageResource{
		RootName:    bucket,
		Path:        objectPath,
		FullPath:    bucket + "/" + objectPath,
		NodeType:    plugin.CatalogKindObject,
		SizeBytes:   sizeBytes,
		Format:      formatName,
		CatalogPath: plugin.ObjectItemPath(engineID, bucket, objectPath),
	}
}

func jsonStringAt(attrs models.JSONMap, section, key string) string {
	return commonJSON.InterfaceString(jsonMapAt(attrs, section)[key])
}

func jsonMapAt(attrs models.JSONMap, section string, keys ...string) map[string]interface{} {
	current := commonJSON.InterfaceMap(attrs[section])
	for _, key := range keys {
		current = commonJSON.InterfaceMap(current[key])
	}
	return current
}

func insertObjectItemForTest(t *testing.T, repo *metaRepo.ScanRepository, tenantID, engineID uint, node *models.MetaNode, bucket, objectPath string, sizeBytes int64) {
	t.Helper()
	dir := metacatalog.ParentObjectPath(objectPath)
	name := path.Base(strings.Trim(objectPath, "/"))
	attrs := models.JSONMap{
		"storage": map[string]interface{}{
			"bucket": bucket,
			"path":   dir,
			"name":   name,
			"size":   sizeBytes,
		},
		"item": map[string]interface{}{
			"layout":    string(format.LayoutSingle),
			"data_type": "media",
			"format":    strings.TrimPrefix(strings.ToLower(path.Ext(objectPath)), "."),
		},
	}
	if _, err := repo.UpsertItemWithDepth(tenantID, engineID, node, "object", name, bucket+"/"+strings.Trim(objectPath, "/"), attrs, nil, &sizeBytes, nil, models.ScannedDepthDeep); err != nil {
		t.Fatalf("insert object item %s: %v", objectPath, err)
	}
}

type objectCatalogScanTestProvider struct {
	staticObjectContentReader
	content       string
	contentByPath map[string]string
}

func (p objectCatalogScanTestProvider) Type() string         { return "object-catalog-scan-test" }
func (p objectCatalogScanTestProvider) DisplayName() string  { return "object catalog scan test" }
func (p objectCatalogScanTestProvider) EngineOrigin() string { return "general" }
func (p objectCatalogScanTestProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.NewObjectCapabilities(p.Type())
}
func (p objectCatalogScanTestProvider) CatalogModel() plugin.CatalogModelSpec {
	return plugin.ObjectCatalogModel()
}
func (p objectCatalogScanTestProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	switch parent.StringPath() {
	case "":
		return []plugin.CatalogEntry{plugin.ObjectBucketCatalogEntry(plugin.ObjectRootPath(9), "addp")}, nil
	case "addp/image":
		return geotiffCatalogEntriesForTest(9, "addp"), nil
	default:
		return nil, nil
	}
}
func (p objectCatalogScanTestProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, pathValue plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	if pathValue.StringPath() == "addp/image" {
		entry := plugin.CatalogEntry{Name: "image", Path: plugin.ObjectDirectoryPath(9, "addp", "image"), Kind: plugin.CatalogKindPrefix, Term: plugin.CatalogTermPrefix, Role: plugin.CatalogRoleBranch}
		return &entry, nil
	}
	for _, entry := range geotiffCatalogEntriesForTest(9, "addp") {
		if entry.Path.StringPath() == pathValue.StringPath() {
			item := entry
			return &item, nil
		}
	}
	return nil, nil
}
func (p objectCatalogScanTestProvider) OpenContent(_ context.Context, _ plugin.ConnectionInfo, pathValue plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	if p.contentByPath != nil {
		if content, ok := p.contentByPath[pathValue.StringPath()]; ok {
			return io.NopCloser(strings.NewReader(content)), nil
		}
	}
	return io.NopCloser(strings.NewReader(p.content)), nil
}

func geotiffCatalogEntriesForTest(engineID uint, bucket string) []plugin.CatalogEntry {
	entries := make([]plugin.CatalogEntry, 0, 4)
	for _, item := range []struct {
		path string
		size int64
	}{
		{"image/srtm_40_01.tif", 100},
		{"image/srtm_40_01.tfw", 10},
		{"image/srtm_40_01.hdr", 20},
		{"image/srtm_40_01.tif.aux.xml", 30},
	} {
		name := path.Base(item.path)
		size := item.size
		entries = append(entries, plugin.CatalogEntry{
			Name: name,
			Path: plugin.ObjectItemPath(engineID, bucket, item.path),
			Kind: plugin.CatalogKindObject,
			Term: plugin.CatalogTermObject,
			Role: plugin.CatalogRoleLeaf,
			Storage: &plugin.CatalogStorageFacts{
				Path:      bucket + "/" + item.path,
				SizeBytes: &size,
			},
		})
	}
	return entries
}

package scanruntime

import (
	"context"
	"io"
	"log/slog"
	"path"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
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
	count, _, err := runtime.persistObjectResources(
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
	count, _, err := runtime.persistObjectResources(
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

	result, err := runtime.ScanPaths(resource, 1, []string{"addp/image"}, nil, models.ScannedDepthDeep, true, nil)
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
	return metacatalog.StorageResource{
		RootName:    bucket,
		Path:        objectPath,
		FullPath:    bucket + "/" + objectPath,
		NodeType:    plugin.CatalogKindObject,
		SizeBytes:   sizeBytes,
		CatalogPath: plugin.ObjectItemPath(engineID, bucket, objectPath),
	}
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
	content string
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

package scanruntime

import (
	"context"
	"io"
	"log/slog"
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
		"a5.shp",
		"a5.shx",
		"a5.dbf",
		"a5.cpg",
	}, nil)
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

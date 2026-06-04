package service

import (
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func TestNormalizedScanRefsAddsPrimaryAndDeduplicates(t *testing.T) {
	t.Parallel()

	refs := normalizedScanRefs(models.ScanRefGroup{
		Primary: "bucket/roads.shp",
		Refs: []models.ScanRef{
			{Path: "bucket/roads.shp", Role: "main", Required: true},
			{Path: "bucket/roads.dbf", Role: "sidecar", Required: true},
			{Path: " ", Role: "sidecar", Required: true},
		},
	})

	want := []models.ScanRef{
		{Path: "bucket/roads.shp", Role: "main", Required: true},
		{Path: "bucket/roads.dbf", Role: "sidecar", Required: true},
	}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %#v, want %#v", refs, want)
	}
}

func TestFileRefsFromScanRefGroup(t *testing.T) {
	t.Parallel()

	files := fileRefsFromScanRefGroup(7, models.ScanRefGroup{
		Primary: "shp/roads.shp",
		Refs: []models.ScanRef{
			{Path: "shp/roads.dbf", Role: "sidecar", Required: true},
		},
	})
	if len(files) != 2 {
		t.Fatalf("files = %#v", files)
	}
	if files[0].Name != "roads.shp" || files[0].Path != "shp/roads.shp" {
		t.Fatalf("primary file = %#v", files[0])
	}
	if files[1].Name != "roads.dbf" || files[1].Path != "shp/roads.dbf" {
		t.Fatalf("sidecar file = %#v", files[1])
	}
}

func TestObjectResourcesFromScanRefGroupRejectsCrossBucketRefs(t *testing.T) {
	t.Parallel()

	_, err := objectResourcesFromScanRefGroup(7, "bucket-a", models.ScanRefGroup{
		Primary: "bucket-a/roads.shp",
		Refs: []models.ScanRef{
			{Path: "bucket-b/roads.dbf", Role: "sidecar", Required: true},
		},
	})
	if err == nil {
		t.Fatal("objectResourcesFromScanRefGroup() should reject cross-bucket refs")
	}
}

func TestSplitObjectRefPath(t *testing.T) {
	t.Parallel()

	bucket, objectPath, err := splitObjectRefPath("/bucket/path/roads.shp")
	if err != nil {
		t.Fatalf("splitObjectRefPath() error = %v", err)
	}
	if bucket != "bucket" || objectPath != "path/roads.shp" {
		t.Fatalf("bucket/object = %q/%q", bucket, objectPath)
	}
}

func TestObjectScanRefGroupsPersistsSingleShapefileItem(t *testing.T) {
	reader := staticObjectContentReader{content: ""}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := &ObjectStorageCatalogScanService{
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		repo: repo,
	}
	resource := &commonModels.Engine{ID: 9, Name: "Object Store", EngineType: reader.Type()}

	result, err := svc.ScanRefGroups(resource, 1, []models.ScanRefGroup{
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

func TestFileScanRefGroupsPersistsSingleShapefileItem(t *testing.T) {
	provider := filesystemScanTestProvider{content: ""}
	pluginRegisterForTest(t, provider)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := &FilesystemCatalogScanService{
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		repo: repo,
	}
	resource := &commonModels.Engine{ID: 26, Name: "Files", EngineType: provider.Type()}

	_, items, _, err := svc.ScanRefGroups(resource, 1, []models.ScanRefGroup{
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
	if items != 1 {
		t.Fatalf("items = %d, want one logical shapefile item", items)
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

func assertShapefileLogicalItem(t *testing.T, attrs models.JSONMap, wantPaths, unexpectedPaths []string) {
	t.Helper()
	if got := commonJSON.String(attrs, "item", "layout"); got != string(format.LayoutMulti) {
		t.Fatalf("item.layout = %q, want multi", got)
	}
	if got := commonJSON.String(attrs, "item", "format"); got != string(format.FormatShapefile) {
		t.Fatalf("item.format = %q, want shapefile", got)
	}
	refs := commonJSON.InterfaceSlice(commonJSON.Section(attrs, "item")["refs"])
	if len(refs) != len(wantPaths) {
		t.Fatalf("item.refs = %#v, want %d refs", refs, len(wantPaths))
	}
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		item := commonJSON.InterfaceMap(ref)
		if path := commonJSON.InterfaceString(item["path"]); path != "" {
			paths = append(paths, path)
		}
	}
	for _, want := range wantPaths {
		if !containsString(paths, want) {
			t.Fatalf("item.refs paths = %#v, want %s", paths, want)
		}
	}
	for _, unexpected := range unexpectedPaths {
		if containsString(paths, unexpected) {
			t.Fatalf("item.refs paths = %#v, must not include non-created ref %s", paths, unexpected)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func pluginRegisterForTest(t *testing.T, enginePlugin plugin.EnginePlugin) {
	t.Helper()
	plugin.Register(enginePlugin)
	t.Cleanup(func() {
		plugin.Unregister(enginePlugin.Type())
	})
}

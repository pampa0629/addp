package scanruntime

import (
	"io"
	"log/slog"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func TestObjectScanRefGroupsPersistsSingleShapefileItem(t *testing.T) {
	reader := staticObjectContentReader{content: ""}
	pluginRegisterForTest(t, reader)

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
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
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonModels.Engine{ID: 26, Name: "Files", EngineType: provider.Type()}

	result, err := svc.ScanRefGroups(resource, 1, []models.ScanRefGroup{
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

package scanruntime

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

func TestFilesystemFailedCompositeForceScanKeepsComponents(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	node, err := repo.UpsertNode(1, 26, nil, "dir", "gis", strPtr("gis"), models.JSONMap{})
	if err != nil {
		t.Fatal(err)
	}
	provider := filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{}, content: "invalid shape"}
	for _, name := range []string{"roads.shp", "roads.shx", "roads.dbf", "outside.txt"} {
		if _, err := repo.UpsertItemWithDepth(1, 26, node, "file", name, "gis/"+name, models.JSONMap{"item": map[string]interface{}{"layout": "single"}}, nil, nil, nil, "basic"); err != nil {
			t.Fatal(err)
		}
		if name != "outside.txt" {
			provider.entriesByPath["gis"] = append(provider.entriesByPath["gis"], plugin.EngineCatalogEntry{Name: name, Path: plugin.FileItemPath(26, "gis/"+name), Kind: "file", Term: "file", Role: plugin.EngineCatalogRoleLeaf})
		}
	}
	runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	_, _, err = runtime.scanDirectory(context.Background(), provider, provider, nil, &commonModels.Engine{ID: 26}, 1, "gis", node, false, "file", "deep", true)
	if err == nil {
		t.Fatal("expected invalid Shapefile failure")
	}
	var count int64
	if err := db.Unscoped().Model(&models.MetaItem{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("failed force scan removed records: %d", count)
	}
	primary, found, err := repo.FindItemByFullName(1, 26, "gis/roads.shp")
	if err != nil || !found {
		t.Fatalf("primary missing: %v", err)
	}
	if commonJSON.String(primary.Attributes, "item", "layout") != "single" || primary.ScannedDepth != "basic" {
		t.Fatal("failed deep scan changed primary")
	}
}

func TestBasicWholeBoundaryUsesCatalogRules(t *testing.T) {
	files := []metaitem.StorageFileRef{{Name: "part-1.parquet", Path: "lake/day=1/part-1.parquet"}, {Name: "part-2.parquet", Path: "lake/day=2/part-2.parquet"}}
	result, err := metaitem.ResolveItems(context.Background(), metaitem.DirectoryResolveInput{DirPath: "lake", RecursiveFiles: files})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Exclusive || len(result.Items) != 1 || result.Items[0].Layout != "whole" || len(result.Claims) != 2 {
		t.Fatalf("list-only whole detection = %#v", result)
	}
	resources := []scanresource.StorageResource{objectResourceForTest(26, "bucket", files[0].Path, 1, "parquet"), objectResourceForTest(26, "bucket", files[1].Path, 1, "parquet")}
	claims, items, warnings := scanflow.DetectObjectCatalogCompositeItems(context.Background(), nil, nil, 26, resources, false)
	if len(warnings) != 0 || len(items) != 1 || items[0].Item.Layout != "whole" || len(claims) != 2 {
		t.Fatalf("object list-only whole detection: items=%#v claims=%v warnings=%v", items, claims, warnings)
	}
}

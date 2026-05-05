package dataitem

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

func TestInferFormatCanonicalizesExplicitExtensionLikeValues(t *testing.T) {
	tests := []struct {
		name           string
		explicitFormat string
		want           string
	}{
		{name: "jpg to jpeg", explicitFormat: "jpg", want: string(format.FormatJPEG)},
		{name: "xlsx to excel", explicitFormat: "xlsx", want: string(format.FormatExcel)},
		{name: "gpkg to geopackage", explicitFormat: "gpkg", want: string(format.FormatGeoPackage)},
		{name: "dot extension", explicitFormat: ".tif", want: string(format.FormatTIFF)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferFormat("fallback.bin", "", tt.explicitFormat)
			if got != tt.want {
				t.Fatalf("InferFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferFormatUsesMIMEBeforeFilenameFallback(t *testing.T) {
	got := InferFormat("unknown.bin", "image/png", "")
	if got != string(format.FormatPNG) {
		t.Fatalf("InferFormat() = %q, want %q", got, format.FormatPNG)
	}
}

func TestInferSingleFileUsesCanonicalFormatForFamily(t *testing.T) {
	item := InferSingleFile(SingleFileInput{
		Name:   "sheet.bin",
		Path:   "bucket/sheet.xlsx",
		Size:   42,
		Format: "xlsx",
	})

	if item.Format != string(format.FormatExcel) {
		t.Fatalf("Format = %q, want %q", item.Format, format.FormatExcel)
	}
	if item.DataFamily != DataFamilyTabular {
		t.Fatalf("DataFamily = %q, want %q", item.DataFamily, DataFamilyTabular)
	}
}

func TestInferSingleFileDetectsContainerComposition(t *testing.T) {
	item := InferSingleFile(SingleFileInput{
		Name: "data.gpkg",
		Path: "bucket/data.gpkg",
		Size: 42,
	})

	if item.CompositionType != CompositionTypeContainerFile {
		t.Fatalf("CompositionType = %q, want %q", item.CompositionType, CompositionTypeContainerFile)
	}
	if item.DataFamily != DataFamilyTabular {
		t.Fatalf("DataFamily = %q, want %q", item.DataFamily, DataFamilyTabular)
	}
}

func TestBuildAttributesWritesPartitionedItemAndStorage(t *testing.T) {
	item := InferSingleFile(SingleFileInput{
		Name:        "roads.geojson",
		Path:        "bucket/roads.geojson",
		Size:        42,
		ContentType: "application/geo+json",
	})

	attrs := BuildAttributes(item)
	itemAttrs := attrs["item"].(map[string]interface{})
	if itemAttrs["composition_type"] != string(CompositionTypeSingleFile) {
		t.Fatalf("item.composition_type = %v, want %s", itemAttrs["composition_type"], CompositionTypeSingleFile)
	}
	if itemAttrs["data_family"] != string(DataFamilyTabular) {
		t.Fatalf("item.data_family = %v, want %s", itemAttrs["data_family"], DataFamilyTabular)
	}
	if itemAttrs["format"] != string(format.FormatGeoJSON) {
		t.Fatalf("item.format = %v, want %s", itemAttrs["format"], format.FormatGeoJSON)
	}
	if attrs["data_family"] != nil || attrs["format"] != nil {
		t.Fatalf("flat item fields should not be written: %#v", attrs)
	}

	storageAttrs := attrs["storage"].(map[string]interface{})
	if storageAttrs["physical_path"] != "bucket/roads.geojson" {
		t.Fatalf("storage.physical_path = %v, want bucket/roads.geojson", storageAttrs["physical_path"])
	}
	if storageAttrs["total_size"] != int64(42) {
		t.Fatalf("storage.total_size = %v, want 42", storageAttrs["total_size"])
	}
}

func TestInferDataFamilyCanonicalizesCommonAliases(t *testing.T) {
	tests := []struct {
		formatName string
		want       DataFamily
	}{
		{formatName: "jpg", want: DataFamilyImage},
		{formatName: "xlsx", want: DataFamilyTabular},
		{formatName: "gpkg", want: DataFamilyTabular},
		{formatName: "orc", want: DataFamilyTabular},
	}

	for _, tt := range tests {
		t.Run(tt.formatName, func(t *testing.T) {
			got := InferDataFamily(tt.formatName, "")
			if got != tt.want {
				t.Fatalf("InferDataFamily() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnclaimedFilesFiltersClaimedPaths(t *testing.T) {
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "/shp/roads.shp"},
		{Name: "roads.shx", Path: "/shp/roads.shx"},
		{Name: "readme.pdf", Path: "/shp/readme.pdf"},
	}

	got := unclaimedFiles(files, ResourceClaimSet{
		"/shp/roads.shp": true,
		"/shp/roads.shx": true,
	})

	if len(got) != 1 || got[0].Path != "/shp/readme.pdf" {
		t.Fatalf("unclaimedFiles() = %#v, want only readme.pdf", got)
	}
}

func TestResolveItemsPassesOnlyUnclaimedFilesToNextDetector(t *testing.T) {
	first := &testScopeDetector{
		priority: 20,
		result: &DetectionResult{
			Items:  []*DetectedItem{{ItemType: "table", CompositionType: CompositionTypeMultiFile, EntryPath: "/shp/roads.shp"}},
			Claims: ResourceClaimSet{"/shp/roads.shp": true},
		},
	}
	second := &testScopeDetector{priority: 10, result: &DetectionResult{Claims: ResourceClaimSet{}}}
	mu.Lock()
	old := detectors
	detectors = []CompositeItemDetector{first, second}
	mu.Unlock()
	defer func() {
		mu.Lock()
		detectors = old
		mu.Unlock()
	}()

	_, err := ResolveItems(context.Background(), DirectoryResolveInput{
		Files: []plugin.FileEntry{
			{Name: "roads.shp", Path: "/shp/roads.shp"},
			{Name: "readme.pdf", Path: "/shp/readme.pdf"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(second.seenFiles) != 1 || second.seenFiles[0].Path != "/shp/readme.pdf" {
		t.Fatalf("second detector saw %#v, want only readme.pdf", second.seenFiles)
	}
}

type testScopeDetector struct {
	priority  int
	result    *DetectionResult
	seenFiles []plugin.FileEntry
}

func (d *testScopeDetector) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	return false
}

func (d *testScopeDetector) ExtractItemInfo(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, dirPath string, files []plugin.FileEntry) (*CompositeItemInfo, error) {
	return nil, nil
}

func (d *testScopeDetector) Priority() int { return d.priority }

func (d *testScopeDetector) ItemType() string { return "test" }

func (d *testScopeDetector) ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
	d.seenFiles = input.Files
	return d.result, nil
}

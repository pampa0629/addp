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

func TestInferSingleResourceUsesCanonicalFormatForFamily(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{
		Name:   "sheet.bin",
		Path:   "bucket/sheet.xlsx",
		Size:   42,
		Format: "xlsx",
	})

	if item.Format != string(format.FormatExcel) {
		t.Fatalf("Format = %q, want %q", item.Format, format.FormatExcel)
	}
	if item.DataType != DataTypeContainer {
		t.Fatalf("DataType = %q, want %q", item.DataType, DataTypeContainer)
	}
	if item.ItemType != "file" {
		t.Fatalf("ItemType = %q, want file", item.ItemType)
	}
}

func TestInferSingleResourceDetectsContainerComposition(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{
		Name: "data.gpkg",
		Path: "bucket/data.gpkg",
		Size: 42,
	})

	if item.Organization != OrganizationSingle {
		t.Fatalf("Organization = %q, want %q", item.Organization, OrganizationSingle)
	}
	if item.DataType != DataTypeContainer {
		t.Fatalf("DataType = %q, want %q", item.DataType, DataTypeContainer)
	}
	if item.ItemType != "file" {
		t.Fatalf("ItemType = %q, want file", item.ItemType)
	}
}

func TestBuildAttributesWritesPartitionedItemAndStorage(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{
		Name:        "roads.geojson",
		Path:        "bucket/roads.geojson",
		Size:        42,
		ContentType: "application/geo+json",
	})

	attrs := BuildAttributes(item)
	itemAttrs := attrs["item"].(map[string]interface{})
	if itemAttrs["organization"] != string(OrganizationSingle) {
		t.Fatalf("item.organization = %v, want %s", itemAttrs["organization"], OrganizationSingle)
	}
	if itemAttrs["data_type"] != string(DataTypeTable) {
		t.Fatalf("item.data_type = %v, want %s", itemAttrs["data_type"], DataTypeTable)
	}
	if itemAttrs["format"] != string(format.FormatGeoJSON) {
		t.Fatalf("item.format = %v, want %s", itemAttrs["format"], format.FormatGeoJSON)
	}
	if attrs["data_type"] != nil || attrs["format"] != nil {
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

func TestInferSingleGeoJSONWritesSpatialCapability(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{Name: "roads.geojson", Path: "roads.geojson"})
	attrs := BuildAttributes(item)
	spatial := attrs["capabilities"].(map[string]interface{})["spatial"].(map[string]interface{})
	if spatial["primary_geometry_column"] != "geometry" || spatial["has_spatial_index"] != false {
		t.Fatalf("capabilities.spatial = %#v", spatial)
	}
	columns := spatial["geometry_columns"].([]map[string]interface{})
	if len(columns) != 1 || columns[0]["srid"] != 4326 || columns[0]["geometry_type"] != "geometry" {
		t.Fatalf("geometry_columns = %#v", columns)
	}
}

func TestInferSingleTIFFWritesRasterSpatialShell(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{Name: "scene.tif", Path: "scene.tif"})
	attrs := BuildAttributes(item)
	spatial := attrs["capabilities"].(map[string]interface{})["spatial"].(map[string]interface{})
	if _, ok := spatial["extent"]; !ok {
		t.Fatalf("capabilities.spatial.extent missing: %#v", spatial)
	}
	if spatial["has_spatial_index"] != false {
		t.Fatalf("capabilities.spatial = %#v", spatial)
	}
}

func TestInferDataTypeCanonicalizesCommonAliases(t *testing.T) {
	tests := []struct {
		formatName string
		want       DataType
	}{
		{formatName: "jpg", want: DataTypeMedia},
		{formatName: "xlsx", want: DataTypeContainer},
		{formatName: "gpkg", want: DataTypeContainer},
		{formatName: "orc", want: DataTypeTable},
	}

	for _, tt := range tests {
		t.Run(tt.formatName, func(t *testing.T) {
			got := InferDataType(tt.formatName, "")
			if got != tt.want {
				t.Fatalf("InferDataType() = %q, want %q", got, tt.want)
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
			Items:  []*DetectedItem{{ItemType: "table", Organization: OrganizationMulti, EntryPath: "/shp/roads.shp"}},
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

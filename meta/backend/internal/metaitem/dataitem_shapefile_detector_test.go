package metaitem

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/dataitem"
	"github.com/jonas-p/go-shp"
)

func TestDetectorDetectsCompleteShapefile(t *testing.T) {
	d := &shapefileItemDetector{}
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "bucket/roads/roads.shp", Size: 10},
		{Name: "roads.shx", Path: "bucket/roads/roads.shx", Size: 20},
		{Name: "roads.dbf", Path: "bucket/roads/roads.dbf", Size: 30},
		{Name: "roads.prj", Path: "bucket/roads/roads.prj", Size: 40},
	}

	if !d.Detect(context.Background(), files, nil) {
		t.Fatal("expected complete shapefile component set to match")
	}

	info, err := d.ExtractItemInfo(context.Background(), nil, nil, 1, "bucket/roads", files)
	if err != nil {
		t.Fatalf("ExtractItemInfo() error = %v", err)
	}
	if info.Organization != dataitem.OrganizationMulti {
		t.Fatalf("Organization = %q, want %q", info.Organization, dataitem.OrganizationMulti)
	}
	if info.Format != "shapefile" {
		t.Fatalf("Format = %q, want shapefile", info.Format)
	}
	if got, want := len(info.ComponentFiles), len(files); got != want {
		t.Fatalf("ComponentFiles len = %d, want %d", got, want)
	}
	if info.Attributes["base_name"] != nil || info.Attributes["has_prj"] != nil || info.Attributes["extensions"] != nil {
		t.Fatalf("shapefile private fields should not be flat: %#v", info.Attributes)
	}
	formatInfo := info.Attributes["format_info"].(map[string]interface{})
	shapefileInfo := formatInfo["shapefile"].(map[string]interface{})
	if shapefileInfo["base_name"] != "roads" || shapefileInfo["has_prj"] != true {
		t.Fatalf("shapefile format info = %#v, want base_name and has_prj", shapefileInfo)
	}
}

func TestDetectorEnrichesShapefileViaFormatProvider(t *testing.T) {
	t.Parallel()

	base := createMetaTestShapefile(t)
	content := map[string][]byte{}
	for _, ext := range []string{".shp", ".shx", ".dbf"} {
		data, err := os.ReadFile(base + ext)
		if err != nil {
			t.Fatalf("read %s: %v", ext, err)
		}
		content["bucket/gis/roads"+ext] = data
	}

	d := &shapefileItemDetector{}
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "bucket/gis/roads.shp", Size: int64(len(content["bucket/gis/roads.shp"]))},
		{Name: "roads.shx", Path: "bucket/gis/roads.shx", Size: int64(len(content["bucket/gis/roads.shx"]))},
		{Name: "roads.dbf", Path: "bucket/gis/roads.dbf", Size: int64(len(content["bucket/gis/roads.dbf"]))},
	}

	info, err := d.ExtractItemInfo(context.Background(), shapefileMapContentReader{content: content}, nil, 1, "bucket/gis", files)
	if err != nil {
		t.Fatalf("ExtractItemInfo() error = %v", err)
	}
	if rowCount := commonJSON.Int64(info.Attributes, "type_info.table", "row_count"); rowCount != 2 {
		t.Fatalf("row_count = %d, want 2", rowCount)
	}
	spatial := commonJSON.Section(info.Attributes, "capabilities.spatial")
	if extent, ok := spatial["extent"].([]float64); !ok || len(extent) != 4 || extent[0] != 1 || extent[1] != 2 || extent[2] != 3 || extent[3] != 4 {
		t.Fatalf("extent = %#v, want shp header bbox", spatial["extent"])
	}
	formatInfo := commonJSON.Section(info.Attributes, "format_info.shapefile")
	if formatInfo["shape_type"] != "Point" {
		t.Fatalf("format_info.shapefile = %#v, want shape_type Point", formatInfo)
	}
}

func TestDetectorRuleDeclaresMultiFileComponents(t *testing.T) {
	d := &shapefileItemDetector{}
	shapefileItemRule := d.Rule()

	if shapefileItemRule.Format != "shapefile" {
		t.Fatalf("Format = %q, want shapefile", shapefileItemRule.Format)
	}
	if shapefileItemRule.Organization != dataitem.OrganizationMulti {
		t.Fatalf("Organization = %q, want multi", shapefileItemRule.Organization)
	}
	if shapefileItemRule.Components == nil {
		t.Fatal("Components missing")
	}
	if shapefileItemRule.Components.EntryExtension != ".shp" {
		t.Fatalf("EntryExtension = %q, want .shp", shapefileItemRule.Components.EntryExtension)
	}
	if shapefileItemRule.Components.AllowRecursive {
		t.Fatal("Shapefile sibling components must not allow recursive matching")
	}
	if err := dataitem.ValidateFormatRule(shapefileItemRule); err != nil {
		t.Fatalf("ValidateFormatRule() error = %v", err)
	}
}

type shapefileMapContentReader struct {
	content map[string][]byte
}

func (r shapefileMapContentReader) Type() string         { return "map" }
func (r shapefileMapContentReader) DisplayName() string  { return "map" }
func (r shapefileMapContentReader) EngineOrigin() string { return "general" }
func (r shapefileMapContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r shapefileMapContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r shapefileMapContentReader) DefaultPort() int                                   { return 0 }
func (r shapefileMapContentReader) RequiredFields() []string                           { return nil }
func (r shapefileMapContentReader) SensitiveFields() []string                          { return nil }
func (r shapefileMapContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r shapefileMapContentReader) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (r shapefileMapContentReader) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	data, ok := r.content[path.StringPath()]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func createMetaTestShapefile(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "roads")
	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{shp.StringField("NAME", 16)})
	row := writer.Write(&shp.Point{X: 1, Y: 2})
	if err := writer.WriteAttribute(int(row), 0, "a"); err != nil {
		t.Fatalf("write attribute failed: %v", err)
	}
	row = writer.Write(&shp.Point{X: 3, Y: 4})
	if err := writer.WriteAttribute(int(row), 0, "b"); err != nil {
		t.Fatalf("write attribute failed: %v", err)
	}
	writer.Close()
	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatalf("rename dbf failed: %v", err)
		}
	}
	return base
}

func TestDetectorResolveItemsDetectsMultipleShapefiles(t *testing.T) {
	d := &shapefileItemDetector{}
	files := []plugin.FileEntry{
		{Name: "farmland.shp", Path: "/shp/farmland.shp", Size: 10},
		{Name: "farmland.shx", Path: "/shp/farmland.shx", Size: 20},
		{Name: "farmland.dbf", Path: "/shp/farmland.dbf", Size: 30},
		{Name: "roads.shp", Path: "/shp/roads.shp", Size: 40},
		{Name: "roads.shx", Path: "/shp/roads.shx", Size: 50},
		{Name: "roads.dbf", Path: "/shp/roads.dbf", Size: 60},
		{Name: "readme.pdf", Path: "/shp/readme.pdf", Size: 70},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		DirPath: "/shp",
		Files:   files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result.Exclusive {
		t.Fatal("shapefile detector must not exclusively claim the directory")
	}
	if got, want := len(result.Items), 2; got != want {
		t.Fatalf("Items len = %d, want %d", got, want)
	}
	if result.Items[0].EntryPath != "/shp/farmland.shp" {
		t.Fatalf("first EntryPath = %q, want /shp/farmland.shp", result.Items[0].EntryPath)
	}
	if result.Items[1].EntryPath != "/shp/roads.shp" {
		t.Fatalf("second EntryPath = %q, want /shp/roads.shp", result.Items[1].EntryPath)
	}
	if !result.Claims["/shp/farmland.dbf"] || !result.Claims["/shp/roads.shx"] {
		t.Fatalf("expected component files to be claimed: %#v", result.Claims)
	}
	if result.Claims["/shp/readme.pdf"] {
		t.Fatalf("unrelated file must not be claimed: %#v", result.Claims)
	}
}

func TestDetectorRejectsIncompleteShapefile(t *testing.T) {
	d := &shapefileItemDetector{}
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "bucket/roads/roads.shp"},
		{Name: "roads.dbf", Path: "bucket/roads/roads.dbf"},
	}

	if d.Detect(context.Background(), files, nil) {
		t.Fatal("expected incomplete shapefile component set to be rejected")
	}
}

func TestDetectorRejectsCrossDirectoryComponents(t *testing.T) {
	d := &shapefileItemDetector{}
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "dataset/roads/roads.shp"},
		{Name: "roads.shx", Path: "dataset/roads/roads.shx"},
		{Name: "roads.dbf", Path: "dataset/roads/attributes/roads.dbf"},
	}

	if d.Detect(context.Background(), files, nil) {
		t.Fatal("expected cross-directory shapefile components to be rejected")
	}
}

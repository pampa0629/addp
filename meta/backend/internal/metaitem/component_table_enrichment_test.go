package metaitem

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/jonas-p/go-shp"
)

func TestCommonDataItemResolverAdaptsMultiItems(t *testing.T) {
	d := &commonDataItemResolver{}
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
		t.Fatal("common data item adapter must not exclusively claim multi items")
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
	if result.Items[0].Organization != dataitem.OrganizationMulti || result.Items[0].DataType != dataitem.DataTypeTable {
		t.Fatalf("first item = %#v, want multi table", result.Items[0])
	}
	if result.Items[0].Format != string(format.FormatShapefile) {
		t.Fatalf("Format = %q, want shapefile from common/format registry", result.Items[0].Format)
	}
	if !result.Claims["/shp/farmland.dbf"] || !result.Claims["/shp/roads.shx"] {
		t.Fatalf("expected component files to be claimed: %#v", result.Claims)
	}
	if result.Claims["/shp/readme.pdf"] {
		t.Fatalf("unrelated file must not be claimed: %#v", result.Claims)
	}
}

func TestCommonDataItemResolverRejectsIncompleteMultiComponents(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "bucket/roads/roads.shp"},
		{Name: "roads.dbf", Path: "bucket/roads/roads.dbf"},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{DirPath: "bucket/roads", Files: files})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 0 || len(result.Claims) != 0 {
		t.Fatalf("ResolveItems() = %#v, want no multi item", result)
	}
}

func TestCommonDataItemResolverRejectsCrossDirectoryComponents(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "dataset/roads/roads.shp"},
		{Name: "roads.shx", Path: "dataset/roads/roads.shx"},
		{Name: "roads.dbf", Path: "dataset/roads/attributes/roads.dbf"},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{DirPath: "dataset/roads", Files: files})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 0 || len(result.Claims) != 0 {
		t.Fatalf("ResolveItems() = %#v, want no multi item", result)
	}
}

func TestCommonDataItemResolverEnrichesComponentTableViaFormatProvider(t *testing.T) {
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

	d := &commonDataItemResolver{}
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "bucket/gis/roads.shp", Size: int64(len(content["bucket/gis/roads.shp"]))},
		{Name: "roads.shx", Path: "bucket/gis/roads.shx", Size: int64(len(content["bucket/gis/roads.shx"]))},
		{Name: "roads.dbf", Path: "bucket/gis/roads.dbf", Size: int64(len(content["bucket/gis/roads.dbf"]))},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: componentMapContentReader{content: content},
		EngineID:      1,
		DirPath:       "bucket/gis",
		Files:         files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("Items = %#v, want one multi item", result.Items)
	}
	attrs := result.Items[0].Attributes
	if rowCount := commonJSON.Int64(attrs, "type_info.table", "row_count"); rowCount != 2 {
		t.Fatalf("row_count = %d, want 2", rowCount)
	}
	spatial := commonJSON.Section(attrs, "capabilities.spatial")
	if extent, ok := spatial["extent"].([]float64); !ok || len(extent) != 4 || extent[0] != 1 || extent[1] != 2 || extent[2] != 3 || extent[3] != 4 {
		t.Fatalf("extent = %#v, want shp header bbox", spatial["extent"])
	}
	formatInfo := commonJSON.Section(attrs, "format_info.shapefile")
	if formatInfo["shape_type"] != "Point" {
		t.Fatalf("format_info.shapefile = %#v, want shape_type Point", formatInfo)
	}
}

type componentMapContentReader struct {
	content map[string][]byte
}

func (r componentMapContentReader) Type() string         { return "map" }
func (r componentMapContentReader) DisplayName() string  { return "map" }
func (r componentMapContentReader) EngineOrigin() string { return "general" }
func (r componentMapContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r componentMapContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (r componentMapContentReader) DefaultPort() int          { return 0 }
func (r componentMapContentReader) RequiredFields() []string  { return nil }
func (r componentMapContentReader) SensitiveFields() []string { return nil }
func (r componentMapContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r componentMapContentReader) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (r componentMapContentReader) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
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

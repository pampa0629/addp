package metaenrich

import (
	"context"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"testing"
)

func TestTIFFAuxiliarySpatialFactsRefresh(t *testing.T) {
	for _, object := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "object"}[object], func(t *testing.T) {
			prefix := "images/"
			if object {
				prefix = "bucket/images/"
			}
			main := prefix + "scene.tif"
			mapper := func(name string) plugin.EngineCatalogPath {
				if object {
					return plugin.ObjectItemPath(7, "bucket", name[len("bucket/"):])
				}
				return plugin.FileItemPath(7, name)
			}
			item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Format: "tiff", DataType: datatype.Media, Layout: format.LayoutMulti, PrimaryContentPath: main, RefList: []dataitem.ItemRef{
				{Path: main, Role: "main", Primary: true, Required: true}, {Path: prefix + "scene.hdr", Role: "header"}, {Path: prefix + "scene.tfw", Role: "world_file"}, {Path: main + ".aux.xml", Role: "auxiliary_metadata"}}}}
			reader := pathContentReader{content: map[string][]byte{main: resourceAttributesTestTIFF(t, 2, 3), prefix + "scene.tfw": []byte("10\n0\n0\n-10\n105\n195\n"), main + ".aux.xml": []byte(`<PAMDataset><SRS>EPSG:4326</SRS></PAMDataset>`)}}
			attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
			run := func() {
				t.Helper()
				if _, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{ContentReader: reader, Item: item, PhysicalPath: main, EngineCatalogPathFor: mapper}); err != nil {
					t.Fatal(err)
				}
			}
			run()
			sp := commonJSON.Section(attrs, "capabilities.spatial")
			box := commonJSON.InterfaceFloat64Slice(sp["extent"])
			if commonJSON.InterfaceInt64(sp["srid"]) != 4326 || len(box) != 4 || box[0] != 100 || box[1] != 170 || box[2] != 120 || box[3] != 200 {
				t.Fatalf("missing TFW/PAM facts: %#v", sp)
			}
			if len(item.RefList) != 4 || commonJSON.InterfaceInt64(commonJSON.Section(attrs, "item")["file_count"]) != 4 {
				t.Fatal("composition changed")
			}
			reader.content[main+".aux.xml"] = []byte(`<PAMDataset><SRS>EPSG:3857</SRS></PAMDataset>`)
			run()
			if commonJSON.InterfaceInt64(commonJSON.Section(attrs, "capabilities.spatial")["srid"]) != 3857 {
				t.Fatal("PAM refresh retained old CRS")
			}
			// The content map still has sidecars, but single must not discover them.
			item.Layout = format.LayoutSingle
			item.RefList = nil
			metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(item))
			run()
			if len(commonJSON.Section(attrs, "capabilities.spatial")) != 0 {
				t.Fatal("removed auxiliary facts survived refresh")
			}
		})
	}
}

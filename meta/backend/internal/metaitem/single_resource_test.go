package metaitem

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

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
	if item.DataType != datatype.Container {
		t.Fatalf("DataType = %q, want %q", item.DataType, datatype.Container)
	}
}

func TestInferSingleResourceDetectsContainerComposition(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{
		Name: "data.gpkg",
		Path: "bucket/data.gpkg",
		Size: 42,
	})

	if item.Layout != format.LayoutSingle {
		t.Fatalf("Layout = %q, want %q", item.Layout, format.LayoutSingle)
	}
	if item.DataType != datatype.Container {
		t.Fatalf("DataType = %q, want %q", item.DataType, datatype.Container)
	}
}

func TestInferSingleResourceDetectsMarkdownAsDocument(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{
		Name: "README.md",
		Path: "bucket/docs/README.md",
		Size: 42,
	})

	if item.Format != string(format.FormatMarkdown) {
		t.Fatalf("Format = %q, want %q", item.Format, format.FormatMarkdown)
	}
	if item.DataType != datatype.Document {
		t.Fatalf("DataType = %q, want %q", item.DataType, datatype.Document)
	}
}

func TestInferSingleResourceDetectsTextAsDocument(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{
		Name: "notes.txt",
		Path: "bucket/docs/notes.txt",
		Size: 42,
	})

	if item.Format != string(format.FormatText) {
		t.Fatalf("Format = %q, want %q", item.Format, format.FormatText)
	}
	if item.DataType != datatype.Document {
		t.Fatalf("DataType = %q, want %q", item.DataType, datatype.Document)
	}
}

func TestInferSingleGeoJSONUsesDeclaredFormatWithoutSpatialFacts(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{Name: "roads.geojson", Path: "roads.geojson"})
	if item.Format != string(format.FormatGeoJSON) {
		t.Fatalf("Format = %q, want %q", item.Format, format.FormatGeoJSON)
	}
	if item.DataType != datatype.Table {
		t.Fatalf("DataType = %q, want %q", item.DataType, datatype.Table)
	}
	if capabilities, ok := item.Attributes["capabilities"].(map[string]interface{}); ok {
		if spatial := capabilities["spatial"]; spatial != nil {
			t.Fatalf("geojson path should not imply spatial capability: %#v", spatial)
		}
	}
}

func TestInferSingleTIFFDoesNotInferSpatialWithoutContentFacts(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{Name: "scene.tif", Path: "scene.tif"})
	if item.DataType != datatype.Media {
		t.Fatalf("DataType = %q, want %q", item.DataType, datatype.Media)
	}
	if capabilities, ok := item.Attributes["capabilities"].(map[string]interface{}); ok {
		if spatial := capabilities["spatial"]; spatial != nil {
			t.Fatalf("tiff path should not imply spatial capability: %#v", spatial)
		}
	}
}

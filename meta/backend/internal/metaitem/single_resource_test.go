package metaitem

import (
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/meta/internal/dataitem"
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
	if item.DataType != dataitem.DataTypeContainer {
		t.Fatalf("DataType = %q, want %q", item.DataType, dataitem.DataTypeContainer)
	}
}

func TestInferSingleResourceDetectsContainerComposition(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{
		Name: "data.gpkg",
		Path: "bucket/data.gpkg",
		Size: 42,
	})

	if item.Organization != dataitem.OrganizationSingle {
		t.Fatalf("Organization = %q, want %q", item.Organization, dataitem.OrganizationSingle)
	}
	if item.DataType != dataitem.DataTypeContainer {
		t.Fatalf("DataType = %q, want %q", item.DataType, dataitem.DataTypeContainer)
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
	if item.DataType != dataitem.DataTypeDocument {
		t.Fatalf("DataType = %q, want %q", item.DataType, dataitem.DataTypeDocument)
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
	if item.DataType != dataitem.DataTypeDocument {
		t.Fatalf("DataType = %q, want %q", item.DataType, dataitem.DataTypeDocument)
	}
}

func TestInferSingleGeoJSONKeepsJSONAsDocumentUntilContentInspection(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{Name: "roads.geojson", Path: "roads.geojson"})
	if item.Format != string(format.FormatJSON) {
		t.Fatalf("Format = %q, want %q", item.Format, format.FormatJSON)
	}
	if item.DataType != dataitem.DataTypeDocument {
		t.Fatalf("DataType = %q, want %q", item.DataType, dataitem.DataTypeDocument)
	}
	if capabilities, ok := item.Attributes["capabilities"].(map[string]interface{}); ok {
		if spatial := capabilities["spatial"]; spatial != nil {
			t.Fatalf("geojson path should not imply spatial capability: %#v", spatial)
		}
	}
}

func TestInferSingleTIFFDoesNotInferSpatialWithoutContentFacts(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{Name: "scene.tif", Path: "scene.tif"})
	if item.DataType != dataitem.DataTypeMedia {
		t.Fatalf("DataType = %q, want %q", item.DataType, dataitem.DataTypeMedia)
	}
	if capabilities, ok := item.Attributes["capabilities"].(map[string]interface{}); ok {
		if spatial := capabilities["spatial"]; spatial != nil {
			t.Fatalf("tiff path should not imply spatial capability: %#v", spatial)
		}
	}
}

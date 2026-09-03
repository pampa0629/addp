package metaenrich

import (
	"context"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func TestEnrichSingleCADItemReadsDWGHeader(t *testing.T) {
	attrs := models.JSONMap{}
	item := cadTestItem(format.FormatDWG)
	content := []byte("AC1032\x00\x00\x00")

	err := EnrichSingleCADItem(
		context.Background(), attrs, rangeBytesContentReader{content: content}, nil, 1,
		item, "cad/drawing.dwg", int64(len(content)), plugin.FileItemPathForEngine(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if item.CAD == nil || item.CAD.SizeBytes == nil || *item.CAD.SizeBytes != int64(len(content)) {
		t.Fatalf("CAD info = %#v", item.CAD)
	}
	if got := commonJSON.String(attrs, "type_info.cad", "drawing_kind"); got != datatype.CADDrawingKind2D {
		t.Fatalf("drawing_kind = %q", got)
	}
	if got := commonJSON.String(attrs, "format_info.dwg", "format_version"); got != "AC1032" {
		t.Fatalf("format_version = %q", got)
	}
}

func TestEnrichSingleCADItemReadsDXFVersion(t *testing.T) {
	attrs := models.JSONMap{}
	item := cadTestItem(format.FormatDXF)
	content := []byte("0\r\nSECTION\r\n2\r\nHEADER\r\n9\r\n$ACADVER\r\n1\r\nAC1014\r\n0\r\nENDSEC\r\n")

	err := EnrichSingleCADItem(
		context.Background(), attrs, rangeBytesContentReader{content: content}, nil, 1,
		item, "cad/drawing.dxf", int64(len(content)), plugin.FileItemPathForEngine(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := commonJSON.String(attrs, "format_info.dxf", "format_version"); got != "AC1014" {
		t.Fatalf("format_version = %q", got)
	}
}

func TestEnrichSingleCADItemDowngradesInvalidCADHeader(t *testing.T) {
	attrs := models.JSONMap{
		"type_info":   map[string]interface{}{"cad": map[string]interface{}{"drawing_kind": "2d"}},
		"format_info": map[string]interface{}{"dwg": map[string]interface{}{"format_version": "AC1032"}},
	}
	item := cadTestItem(format.FormatDWG)

	err := EnrichSingleCADItem(
		context.Background(), attrs, rangeBytesContentReader{content: []byte("not a drawing")}, nil, 1,
		item, "cad/drawing.dwg", 13, plugin.FileItemPathForEngine(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if item.DataType != datatype.Unknown || item.Format != string(format.FormatUnknown) || item.CAD != nil {
		t.Fatalf("item = %#v", item)
	}
	if commonJSON.Section(attrs, "type_info.cad") != nil || commonJSON.Section(attrs, "format_info.dwg") != nil {
		t.Fatalf("stale CAD attributes remain: %#v", attrs)
	}
}

func cadTestItem(formatType format.FormatType) *metaitem.DetectedItem {
	return &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{
		Layout:   format.LayoutSingle,
		DataType: datatype.CAD,
		Format:   string(formatType),
	}}
}

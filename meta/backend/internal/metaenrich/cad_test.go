package metaenrich

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type stubCADInspector struct {
	result *CADInspection
	err    error
}

func (s stubCADInspector) InspectCAD(context.Context, *commonModels.Engine, uint, string, int64) (*CADInspection, error) {
	return s.result, s.err
}

func TestEnrichSingleCADItemWritesTypeAndFormatInfo(t *testing.T) {
	entityCount := int64(12)
	attrs := models.JSONMap{}
	item := &metaitem.DetectedItem{}
	item.Layout = format.LayoutSingle
	item.DataType = datatype.CAD
	item.Format = string(format.FormatDWG)
	err := EnrichSingleCADItem(context.Background(), attrs, stubCADInspector{result: &CADInspection{
		CAD:        &datatype.CADInfo{DrawingKind: datatype.CADDrawingKind2D, EntityCount: &entityCount},
		FormatInfo: map[string]interface{}{"format_version": "AC1032", "geometry_traversed": false},
	}}, &commonModels.Engine{ID: 1}, 2, item, "drawing.dwg", 128)
	if err != nil {
		t.Fatal(err)
	}
	if got := commonJSON.String(attrs, "type_info.cad", "drawing_kind"); got != datatype.CADDrawingKind2D {
		t.Fatalf("drawing_kind = %q", got)
	}
	if got := commonJSON.String(attrs, "format_info.dwg", "format_version"); got != "AC1032" {
		t.Fatalf("format_version = %q", got)
	}
}

func TestEnrichSingleCADItemRequiresInspector(t *testing.T) {
	item := &metaitem.DetectedItem{}
	item.Layout = format.LayoutSingle
	item.DataType = datatype.CAD
	item.Format = string(format.FormatDWG)
	if err := EnrichSingleCADItem(context.Background(), models.JSONMap{}, nil, &commonModels.Engine{ID: 1}, 2, item, "drawing.dwg", 128); err == nil {
		t.Fatal("expected missing inspector error")
	}
}

package service

import "testing"

func TestNormalizeCADInspectionMapsProviderFacts(t *testing.T) {
	inspection, err := normalizeCADInspection(map[string]interface{}{
		"schema_version": "addp.cad.inspect/v1",
		"format":         "dwg",
		"format_version": "AC1032",
		"drawing": map[string]interface{}{
			"drawing_kind": "2d", "layer_count": 4,
			"bounds_2d": map[string]interface{}{"min_x": 1, "min_y": 2, "max_x": 3, "max_y": 4},
		},
		"interpretation": map[string]interface{}{
			"dataset_count": 4, "interpreted_record_count": 99,
			"provider": "supermap_iobjects_cpp", "provider_version": "12.1",
			"normalized_geometry": true, "geometry_traversed": false, "scan_complete": true,
		},
	}, "dwg", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.CAD == nil || inspection.CAD.EntityCount == nil || *inspection.CAD.EntityCount != 99 {
		t.Fatalf("CAD info = %#v", inspection.CAD)
	}
	if inspection.CAD.Bounds2D == nil || inspection.CAD.Bounds2D.MaxX == nil || *inspection.CAD.Bounds2D.MaxX != 3 {
		t.Fatalf("bounds = %#v", inspection.CAD.Bounds2D)
	}
	if inspection.FormatInfo["geometry_traversed"] == nil {
		t.Fatalf("format info = %#v", inspection.FormatInfo)
	}
}

func TestNormalizeCADInspectionAcceptsDXF(t *testing.T) {
	inspection, err := normalizeCADInspection(map[string]interface{}{
		"schema_version": "addp.cad.inspect/v1",
		"format":         "dxf",
		"format_version": "AC1014",
		"drawing":        map[string]interface{}{"drawing_kind": "2d"},
		"interpretation": map[string]interface{}{"geometry_traversed": false},
	}, "dxf", 256)
	if err != nil {
		t.Fatal(err)
	}
	if inspection == nil || inspection.CAD == nil || inspection.FormatInfo["format_version"] != "AC1014" {
		t.Fatalf("inspection = %#v", inspection)
	}
}

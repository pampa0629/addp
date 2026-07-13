package datatype

import "testing"

func TestCADInfoPayloadNormalizesFacts(t *testing.T) {
	info := CADInfoFromPayload(map[string]interface{}{
		"drawing_kind": " 2d ", "unit": " millimeter ", "entity_count": int64(42),
		"layer_count": int64(-1), "has_model_space": true,
		"bounds_2d": map[string]interface{}{"min_x": 1.5},
	})
	if info == nil {
		t.Fatal("CADInfoFromPayload() = nil")
	}
	if info.DrawingKind != CADDrawingKind2D || info.Unit != "millimeter" {
		t.Fatalf("drawing_kind/unit = %q/%q", info.DrawingKind, info.Unit)
	}
	if info.EntityCount == nil || *info.EntityCount != 42 {
		t.Fatalf("EntityCount = %v", info.EntityCount)
	}
	if info.LayerCount != nil {
		t.Fatalf("LayerCount = %v, want nil", info.LayerCount)
	}
	if info.HasModelSpace == nil || !*info.HasModelSpace {
		t.Fatalf("HasModelSpace = %v", info.HasModelSpace)
	}
	if info.Bounds2D == nil || info.Bounds2D.MinX == nil || *info.Bounds2D.MinX != 1.5 {
		t.Fatalf("Bounds2D = %#v", info.Bounds2D)
	}
}

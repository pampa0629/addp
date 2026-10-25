package metaattr

import (
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
	"testing"
)

func TestDAEBasicFailureRemovesOldCompleteRefsWithoutErasingDeepFacts(t *testing.T) {
	previous := models.JSONMap{"item": map[string]interface{}{"format": "dae"}, "format_info": map[string]interface{}{"dae": map[string]interface{}{"scan_complete": true, "texture_refs": []string{"old.png"}, "mesh_count": int64(7)}}}
	failed := models.JSONMap{"item": map[string]interface{}{"format": "dae"}, "format_info": map[string]interface{}{"dae": map[string]interface{}{"scan_complete": false}}}
	MergeBasicModelCompositionSnapshot(previous, failed)
	native := commonJSON.Section(previous, "format_info.dae")
	if native["scan_complete"] != false || native["texture_refs"] != nil || native["mesh_count"] != int64(7) {
		t.Fatalf("failed basic scan retained stale authority: %+v", native)
	}
	empty := models.JSONMap{"item": map[string]interface{}{"format": "dae"}, "format_info": map[string]interface{}{"dae": map[string]interface{}{"scan_complete": true, "texture_refs": []string{}}}}
	MergeBasicModelCompositionSnapshot(previous, empty)
	native = commonJSON.Section(previous, "format_info.dae")
	refs, ok := native["texture_refs"].([]string)
	if native["scan_complete"] != true || !ok || refs == nil || len(refs) != 0 || native["mesh_count"] != int64(7) {
		t.Fatal("known-empty composition was confused with unknown", native)
	}
}

func TestOBJBasicFailureReplacesCompositionWithoutErasingDeepGeometryStatus(t *testing.T) {
	previous := models.JSONMap{"item": map[string]any{"format": "obj"}, "format_info": map[string]any{"obj": map[string]any{"composition_complete": true, "material_libraries": []any{}, "scan_complete": true, "vertex_count": int64(6)}}}
	observed := models.JSONMap{"item": map[string]any{"format": "obj"}, "format_info": map[string]any{"obj": map[string]any{"composition_complete": false}}}
	MergeBasicModelCompositionSnapshot(previous, observed)
	native := commonJSON.Section(previous, "format_info.obj")
	if native["composition_complete"] != false || native["material_libraries"] != nil || native["scan_complete"] != true || native["vertex_count"] != int64(6) {
		t.Fatalf("stale or lost facts: %+v", native)
	}
}

func TestGLBBasicFailureReplacesPriorCompleteWhilePreservingDeepFacts(t *testing.T) {
	previous := models.JSONMap{"item": map[string]any{"format": "glb"}, "format_info": map[string]any{"glb": map[string]any{"composition_complete": true, "scan_complete": true, "mesh_count": int64(2)}}}
	observed := models.JSONMap{"item": map[string]any{"format": "glb"}, "format_info": map[string]any{"glb": map[string]any{"composition_complete": false}}}
	MergeBasicModelCompositionSnapshot(previous, observed)
	native := commonJSON.Section(previous, "format_info.glb")
	if native["composition_complete"] != false || native["mesh_count"] != int64(2) {
		t.Fatal("basic lost independent observation", native)
	}
}

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

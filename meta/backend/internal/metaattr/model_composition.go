package metaattr

import (
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
)

// MergeBasicModelCompositionSnapshot keeps unrelated deep native facts
// while replacing the complete composition observation, including its failure.
// In particular, an incomplete observation must remove the previous refs.
func MergeBasicModelCompositionSnapshot(previous, observed models.JSONMap) {
	formatName := commonJSON.String(observed, "item", "format")
	if (formatName != "3ds" && formatName != "dae" && formatName != "obj" && formatName != "glb" && formatName != "fbx") || commonJSON.String(previous, "item", "format") != formatName {
		return
	}
	next := commonJSON.Section(observed, "format_info."+formatName)
	completeKey, refsKey := "scan_complete", "texture_refs"
	if formatName == "obj" {
		completeKey, refsKey = "composition_complete", "material_libraries"
	}
	if formatName == "glb" || formatName == "fbx" {
		completeKey = "composition_complete"
	}
	if _, ok := next[completeKey].(bool); !ok {
		return
	}
	merged := map[string]interface{}{}
	for key, value := range commonJSON.Section(previous, "format_info."+formatName) {
		if key != refsKey && key != completeKey && !(formatName == "fbx" && key == "profile") {
			merged[key] = value
		}
	}
	for key, value := range next {
		merged[key] = value
	}
	// Replace the previous namespace before the ordinary attribute merge, whose
	// omission semantics deliberately retain existing keys. Otherwise a failed
	// observation without texture_refs would keep the old declaration array.
	ReplaceAttributeNamespace(previous, "format_info", formatName, merged)
}

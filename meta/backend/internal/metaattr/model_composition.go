package metaattr

import (
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
)

// MergeBasicModelCompositionSnapshot keeps unrelated deep native facts
// while replacing the complete composition observation, including its failure.
// In particular, an incomplete observation must remove the previous refs.
func MergeBasicModelCompositionSnapshot(previous, observed models.JSONMap) {
	if commonJSON.String(previous, "item", "format") != "3ds" || commonJSON.String(observed, "item", "format") != "3ds" {
		return
	}
	next := commonJSON.Section(observed, "format_info.3ds")
	if _, ok := next["scan_complete"].(bool); !ok {
		return
	}
	merged := map[string]interface{}{}
	for key, value := range commonJSON.Section(previous, "format_info.3ds") {
		if key != "texture_refs" && key != "scan_complete" {
			merged[key] = value
		}
	}
	for key, value := range next {
		merged[key] = value
	}
	// Replace the previous namespace before the ordinary attribute merge, whose
	// omission semantics deliberately retain existing keys. Otherwise a failed
	// observation without texture_refs would keep the old declaration array.
	ReplaceAttributeNamespace(previous, "format_info", "3ds", merged)
}

package preview

import (
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

func applyWholeContainerPreview(req *PreviewRequest, object *models.ObjectPreview) bool {
	if req == nil || object == nil || itemLayoutFromMetaAttributes(req.Attributes) != "whole" ||
		itemDataTypeFromMetaAttributes(req.Attributes) != "container" || req.ChildName != "" || req.RefPath != "" || req.NestedChildPath != "" {
		return false
	}
	scope := strings.TrimSpace(req.ScopePath)
	if scope == "" {
		return false
	}
	size := commonJSON.Int64(req.Attributes, "storage", "total_size")
	dir, name := splitFSPath(scope)
	content := containerPreviewContentFromMetaAttributes(req.Attributes, size, dir, name)
	if content == nil {
		// Basic identity recognition does not prove that the container is empty.
		container := commonJSON.Section(req.Attributes, "type_info.container")
		children, hasChildren := container["children"].([]interface{})
		count, hasCount := container["child_count"]
		zeroCount := count == float64(0) || count == int(0) || count == int64(0)
		indexAvailable := strings.EqualFold(req.ScannedDepth, "deep") && hasChildren && len(children) == 0 && hasCount && zeroCount
		content = objectcontent.DecoratePreviewContent(&models.ObjectPreviewContent{
			Kind: models.ObjectPreviewKindContainer,
			JSON: map[string]interface{}{
				"format":   string(formatTypeFromMetaAttributes(req.Attributes)),
				"children": []interface{}{},
				"summary":  map[string]interface{}{"index_available": indexAvailable, "size_bytes": size},
			},
			Metadata: map[string]interface{}{"source": "meta", "path": dir, "name": name},
		})
	}
	object.Path, object.StorageRef, object.SizeBytes = scope, scope, size
	object.ContentType = "application/x-directory"
	object.Content = content
	return true
}

func containerPreviewContentFromMetaAttributes(attrs map[string]interface{}, sizeBytes int64, path, name string) *models.ObjectPreviewContent {
	previewJSON := objectcontent.BuildContainerPreviewFromMetaAttributes(attrs, sizeBytes)
	if previewJSON == nil {
		return nil
	}
	return objectcontent.DecoratePreviewContent(&models.ObjectPreviewContent{
		Kind: models.ObjectPreviewKindContainer,
		JSON: previewJSON,
		Metadata: map[string]interface{}{
			"size_bytes": sizeBytes,
			"path":       path,
			"name":       name,
			"source":     "meta",
		},
	})
}

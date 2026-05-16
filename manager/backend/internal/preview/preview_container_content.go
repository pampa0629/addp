package preview

import (
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

func containerPreviewContentFromMetaAttributes(attrs map[string]interface{}, sizeBytes int64, path, name string) *models.ObjectPreviewContent {
	previewJSON := objectcontent.BuildContainerPreviewFromAttributes(attrs, sizeBytes)
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

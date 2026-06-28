package preview

import (
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

func applyOSGBScenePreviewPrompt(attrs map[string]interface{}, object *models.ObjectPreview) bool {
	if object == nil || formatTypeFromMetaAttributes(attrs) != format.FormatOSGBScene {
		return false
	}
	object.Content = objectcontent.DecoratePreviewContent(&models.ObjectPreviewContent{
		Kind:             models.ObjectPreviewKindUnsupported,
		PreviewMaterial:  models.PreviewMaterialUnsupported,
		FrontendRenderer: models.ObjectPreviewKindUnsupported,
		Metadata: map[string]interface{}{
			"source_format":     string(format.FormatOSGBScene),
			"preview_material":  models.PreviewMaterialUnsupported,
			"frontend_renderer": models.ObjectPreviewKindUnsupported,
			"action":            "create_task",
			"task_type":         commonExecution.TaskTypeModel3DTilesGeneration,
		},
	})
	return true
}

package objectcontent

import (
	"context"
	"strings"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
)

type cadContentHandler struct {
	baseContentHandler
}

func (h *cadContentHandler) Handle(_ context.Context, req *ObjectContentRequest, _ ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	metadata := buildPreviewMetadata(req, 0)
	metadata["source_format"] = string(format.FormatDWG)
	previewURL := ""
	if req != nil {
		previewURL = strings.TrimSpace(req.PreviewURL)
	}
	if previewURL == "" {
		metadata["preview_reason"] = "requires_cad_preview_generation"
		metadata["preview_artifact_status"] = "missing"
		metadata["preview_artifact_task_type"] = commonExecution.TaskTypeCADPreviewGeneration
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:             models.ObjectPreviewKindCAD,
			PreviewMaterial:  models.PreviewMaterialUnsupported,
			FrontendRenderer: models.ObjectPreviewKindUnsupported,
			Metadata:         metadata,
		}), false, nil
	}
	metadata["preview_artifact_status"] = "ready"
	metadata["preview_artifact_task_type"] = ""
	metadata["manifest_url"] = previewURL
	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind:             models.ObjectPreviewKindCAD,
		PreviewMaterial:  models.PreviewMaterialURL,
		FrontendRenderer: models.ObjectPreviewKindCAD,
		URL:              previewURL,
		Metadata:         metadata,
	}), false, nil
}

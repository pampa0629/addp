package objectcontent

import (
	"context"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
)

type cadContentHandler struct {
	baseContentHandler
}

func (h *cadContentHandler) Handle(_ context.Context, req *ObjectContentRequest, _ ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	metadata := buildPreviewMetadata(req, 0)
	sourceFormat := format.FormatUnknown
	if req != nil {
		sourceFormat = format.NormalizeFormat(req.Format)
	}
	metadata["source_format"] = string(sourceFormat)
	previewURL := ""
	if req != nil {
		previewURL = strings.TrimSpace(req.PreviewURL)
	}
	if previewURL == "" {
		metadata["preview_reason"] = "preview_url_missing"
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:             models.ObjectPreviewKindCAD,
			PreviewMaterial:  models.PreviewMaterialUnsupported,
			FrontendRenderer: models.ObjectPreviewKindUnsupported,
			Metadata:         metadata,
		}), false, nil
	}
	metadata["source_url"] = previewURL
	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind:             models.ObjectPreviewKindCAD,
		PreviewMaterial:  models.PreviewMaterialURL,
		FrontendRenderer: models.ObjectPreviewKindCAD,
		URL:              previewURL,
		Metadata:         metadata,
	}), false, nil
}

package preview

import (
	"context"
	"strings"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

type Model3DQuickViewLookup interface {
	GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.Model3DQuickView, error)
}

func applyModel3DQuickViewPreview(ctx context.Context, lookup Model3DQuickViewLookup, req *PreviewRequest, object *models.ObjectPreview) (bool, error) {
	if lookup == nil || req == nil || object == nil {
		return false, nil
	}
	sourceFormat := model3DQuickViewSourceFormat(req.Attributes)
	if sourceFormat == format.FormatUnknown {
		return false, nil
	}
	if req.TenantID == nil || *req.TenantID == 0 || strings.TrimSpace(req.ItemFingerprint) == "" {
		object.Content = model3DQuickViewUnsupportedContent(sourceFormat, "identity_unresolved", req.ItemFingerprint, 0)
		return true, nil
	}
	result, err := lookup.GetLatestReadyByFingerprint(ctx, *req.TenantID, req.ItemFingerprint)
	if err != nil {
		return false, err
	}
	if result == nil || strings.TrimSpace(result.ContentURL) == "" {
		object.Content = model3DQuickViewUnsupportedContent(sourceFormat, "quick_view_missing", req.ItemFingerprint, 0)
		return true, nil
	}

	object.URL = strings.TrimSpace(result.ContentURL)
	object.ContentType = "model/gltf-binary"
	object.Content = objectcontent.DecoratePreviewContent(&models.ObjectPreviewContent{
		Kind:             models.ObjectPreviewKindModel3D,
		PreviewMaterial:  models.PreviewMaterialURL,
		FrontendRenderer: models.ObjectPreviewKindModel3D,
		URL:              object.URL,
		Metadata: model3DQuickViewMetadata(sourceFormat, "ready", req.ItemFingerprint, result.ID, map[string]interface{}{
			"file_name":  result.FileName,
			"size_bytes": result.SizeBytes,
		}),
	})
	return true, nil
}

func model3DQuickViewSourceFormat(attrs map[string]interface{}) format.FormatType {
	if !strings.EqualFold(itemDataTypeFromMetaAttributes(attrs), "model_3d") {
		return format.FormatUnknown
	}
	formatType := formatTypeFromMetaAttributes(attrs)
	layout := itemLayoutFromMetaAttributes(attrs)
	switch formatType {
	case format.FormatOSGB:
		if layout == "" || layout == string(format.LayoutSingle) {
			return formatType
		}
	case format.FormatGLTF:
		if layout == string(format.LayoutMulti) {
			return formatType
		}
	case format.FormatFBX:
		if layout == "" || layout == string(format.LayoutSingle) {
			return formatType
		}
	case format.FormatOBJ:
		if layout == "" || layout == string(format.LayoutSingle) {
			return formatType
		}
	default:
		return format.FormatUnknown
	}
	return format.FormatUnknown
}

func model3DQuickViewUnsupportedContent(sourceFormat format.FormatType, status, itemFingerprint string, resultID uint) *models.ObjectPreviewContent {
	return objectcontent.DecoratePreviewContent(&models.ObjectPreviewContent{
		Kind:             models.ObjectPreviewKindUnsupported,
		PreviewMaterial:  models.PreviewMaterialUnsupported,
		FrontendRenderer: models.ObjectPreviewKindUnsupported,
		Metadata: model3DQuickViewMetadata(sourceFormat, status, itemFingerprint, resultID, map[string]interface{}{
			"action":    "create_task",
			"task_type": commonExecution.TaskTypeModel3DQuickViewGeneration,
		}),
	})
}

func model3DQuickViewMetadata(sourceFormat format.FormatType, status, itemFingerprint string, resultID uint, extra map[string]interface{}) map[string]interface{} {
	metadata := map[string]interface{}{
		"source_format":        string(sourceFormat),
		"quick_view_status":    status,
		"item_fingerprint":     strings.TrimSpace(itemFingerprint),
		"preview_material":     models.PreviewMaterialUnsupported,
		"frontend_renderer":    models.ObjectPreviewKindUnsupported,
		"quick_view_task_type": commonExecution.TaskTypeModel3DQuickViewGeneration,
	}
	if resultID > 0 {
		metadata["quick_view_id"] = resultID
		metadata["quick_view_status"] = "ready"
		metadata["preview_material"] = models.PreviewMaterialURL
		metadata["frontend_renderer"] = models.ObjectPreviewKindModel3D
	}
	for key, value := range extra {
		if strings.TrimSpace(key) == "" || value == nil || commonJSON.InterfaceString(value) == "" {
			continue
		}
		metadata[key] = value
	}
	return metadata
}

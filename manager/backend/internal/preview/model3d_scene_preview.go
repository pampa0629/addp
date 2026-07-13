package preview

import (
	"path"
	"strings"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

func applyS3MScenePreview(attrs map[string]interface{}, object *models.ObjectPreview, engineID uint, bucket, scopePath string) bool {
	if object == nil || formatTypeFromMetaAttributes(attrs) != format.FormatS3M {
		return false
	}
	manifestPath := s3mManifestObjectPath(bucket, scopePath, attrs)
	if strings.TrimSpace(manifestPath) == "" {
		object.Content = objectcontent.DecoratePreviewContent(&models.ObjectPreviewContent{
			Kind: models.ObjectPreviewKindUnsupported,
			Metadata: map[string]interface{}{
				"source_format":  string(format.FormatS3M),
				"preview_reason": "manifest_ref_missing",
			},
		})
		return true
	}
	storageRef := strings.Trim(manifestPath, "/")
	if bucket != "" && !strings.HasPrefix(storageRef, strings.Trim(bucket, "/")+"/") {
		storageRef = strings.Trim(path.Join(bucket, storageRef), "/")
	}
	url := buildStorageAssetURL(engineID, storageRef)
	metadata := map[string]interface{}{
		"source_format":     string(format.FormatS3M),
		"manifest_ref":      strings.TrimPrefix(strings.Trim(manifestPath, "/"), strings.Trim(scopePath, "/")+"/"),
		"preview_material":  models.PreviewMaterialURL,
		"frontend_renderer": string(format.FormatS3M),
		"manifest_encoding": commonJSON.InterfaceString(commonJSON.Section(attrs, "format_info.s3m")["manifest_encoding"]),
		"tile_extension":    commonJSON.InterfaceString(commonJSON.Section(attrs, "format_info.s3m")["tile_extension"]),
	}
	object.StorageRef = storageRef
	object.URL = url
	object.ContentType = "application/vnd.supermap.s3m-config"
	object.NodeType = "directory"
	object.Content = objectcontent.DecoratePreviewContent(&models.ObjectPreviewContent{
		Kind:             models.ObjectPreviewKindModel3D,
		PreviewMaterial:  models.PreviewMaterialURL,
		FrontendRenderer: string(format.FormatS3M),
		URL:              url,
		Metadata:         metadata,
	})
	return true
}

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

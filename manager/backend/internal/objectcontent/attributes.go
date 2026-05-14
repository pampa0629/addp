package objectcontent

import (
	"strings"

	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
)

const (
	maxTextPreviewBytes      = 256 * 1024
	maxJSONPreviewBytes      = 512 * 1024
	maxGeoJSONPreview        = 1024 * 1024
	maxPDFPreviewBytes       = 20 * 1024 * 1024
	maxDOCXPreviewBytes      = 100 * 1024 * 1024
	maxWPSPreviewBytes       = 100 * 1024 * 1024
	maxPPTXPreviewBytes      = 100 * 1024 * 1024
	maxContainerPreviewBytes = 30 * 1024 * 1024
	maxImagePreviewBytes     = 20 * 1024 * 1024
)

func stringAttribute(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	for _, section := range attributeSectionsForKey(key) {
		if value := sectionStringAttribute(attrs, section, key); value != "" {
			return value
		}
	}
	return ""
}

func attributeSectionsForKey(key string) []string {
	switch key {
	case "organization", "data_type", "format", "component_files", "file_count", "scope_exclusive", "claim_policy":
		return []string{"item"}
	case "bucket", "path", "name", "physical_path", "size_bytes", "size", "total_size", "content_type", "last_modified_at", "etag":
		return []string{"storage"}
	case "fields", "primary_key", "indexes", "row_count", "document_count":
		return []string{"type_info.table"}
	case "width", "height", "duration", "codec", "page_count", "word_count":
		return []string{"type_info.media", "type_info.document"}
	case "spatial", "geometry_columns", "primary_geometry_column", "extent", "has_spatial_index":
		return []string{"capabilities.spatial"}
	case "metadata_extracted", "extractor_available", "extracted_metadata", "plain_text_preview":
		return []string{"capabilities.extraction"}
	default:
		return nil
	}
}

func sectionStringAttribute(attrs map[string]interface{}, section, key string) string {
	if sectionAttrs := commonJSON.Section(attrs, section); len(sectionAttrs) > 0 {
		return commonJSON.InterfaceString(sectionAttrs[key])
	}
	return ""
}

func interfaceSlice(value interface{}) []interface{} {
	switch typed := value.(type) {
	case []interface{}:
		return typed
	case []map[string]interface{}:
		result := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

func rawMapAttribute(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	case models.JSONMap:
		return map[string]interface{}(typed)
	default:
		return nil
	}
}

func normalizeFileTableFormat(formatName string) format.FormatType {
	normalized := strings.ToLower(strings.TrimSpace(formatName))
	if normalized == "" {
		return format.FormatUnknown
	}
	if byExt := format.DetectFormat("file."+strings.TrimPrefix(normalized, "."), nil); byExt != format.FormatUnknown {
		return byExt
	}
	if byMime := format.MIMEToFormat(normalized); byMime != format.FormatUnknown {
		return byMime
	}
	return format.FormatType(normalized)
}

func isGenericContentType(contentType string) bool {
	switch contentType {
	case "", "application/octet-stream", "binary/octet-stream", "application/download", "application/force-download":
		return true
	}
	if strings.HasPrefix(contentType, "application/x-msdownload") {
		return true
	}
	if !strings.Contains(contentType, "/") {
		return true
	}
	return false
}

func IsContainerFormat(formatName string) bool {
	return isContainerObjectContentFormat(formatName)
}

func DecoratePreviewContent(content *models.ObjectPreviewContent) *models.ObjectPreviewContent {
	return decoratePreviewContent(content)
}

func BuildContainerPreviewFromAttributes(attrs map[string]interface{}, sizeBytes int64) map[string]interface{} {
	return buildContainerPreviewFromAttributes(attrs, sizeBytes)
}

func ResolveContainerAttributeChildrenForPreview(formatName string, children []interface{}) *containerPreviewChildren {
	return resolveContainerAttributeChildrenForPreview(formatName, children)
}

func BuildContainerPreviewFromInfo(info *format.ContainerInfo, fallbackFormat string) map[string]interface{} {
	return buildContainerPreviewFromContainerInfo(info, fallbackFormat)
}

func ContainerInfoTruncated(info *format.ContainerInfo) bool {
	return containerInfoTruncated(info)
}

func BuildContainerMetadata(info *format.ContainerInfo, req *ObjectContentRequest, formatType format.FormatType) map[string]interface{} {
	return buildContainerMetadataMap(info, req, formatType)
}

func InferContentType(objectPath, contentType string) string {
	ctLower := strings.ToLower(strings.TrimSpace(contentType))
	if ctLower != "" && !isGenericContentType(ctLower) {
		return contentType
	}

	if guessed := format.GuessContentType(objectPath, nil); guessed != "" && !isGenericContentType(guessed) {
		return guessed
	}

	if contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

func splitDirectories(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	normalized := strings.ReplaceAll(spec, ";", ",")
	parts := strings.Split(normalized, ",")
	var paths []string
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

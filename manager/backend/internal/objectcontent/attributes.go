package objectcontent

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
)

const (
	maxTextPreviewBytes      = 256 * 1024
	maxJSONPreviewBytes      = 512 * 1024
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
	case "layout", "data_type", "format", "refs", "file_count", "scope_exclusive", "claim_policy":
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

func ContainerChildInfoFromMap(child map[string]interface{}) datatype.ContainerChildInfo {
	if child == nil {
		child = map[string]interface{}{}
	}
	name := strings.TrimSpace(commonJSON.InterfaceString(child["name"]))
	childKind := strings.TrimSpace(commonJSON.InterfaceString(child["child_kind"]))
	dataType := datatype.ParseDataType(commonJSON.InterfaceString(child["data_type"]))
	native := cloneInterfaceMap(rawMapAttribute(child["native"]))

	rowCountValue := commonJSON.InterfaceInt64(child["row_count"])
	var rowCount *int64
	if rowCountValue > 0 {
		rowCount = &rowCountValue
	}
	columnCountValue := int(commonJSON.InterfaceInt64(child["column_count"]))
	var columnCount *int
	if columnCountValue > 0 {
		columnCount = &columnCountValue
	}
	var hasHeader *bool
	if _, ok := child["has_header"]; ok {
		value := commonJSON.InterfaceBool(child["has_header"])
		hasHeader = &value
	}

	return datatype.ContainerChildInfo{
		Name:        name,
		ChildKind:   childKind,
		DataType:    dataType,
		Format:      strings.TrimSpace(commonJSON.InterfaceString(child["format"])),
		Refs:        containerChildRefsFromMap(child),
		RowCount:    rowCount,
		ColumnCount: columnCount,
		HasHeader:   hasHeader,
		Native:      native,
	}
}

func containerChildRefsFromMap(child map[string]interface{}) []datatype.ContainerChildRef {
	values := interfaceSlice(child["refs"])
	if len(values) == 0 {
		return nil
	}
	refs := make([]datatype.ContainerChildRef, 0, len(values))
	for _, value := range values {
		ref := rawMapAttribute(value)
		if len(ref) == 0 {
			continue
		}
		path := strings.TrimSpace(commonJSON.InterfaceString(ref["path"]))
		if path == "" {
			continue
		}
		refs = append(refs, datatype.ContainerChildRef{
			Role:      strings.TrimSpace(commonJSON.InterfaceString(ref["role"])),
			Path:      path,
			Required:  commonJSON.InterfaceBool(ref["required"]),
			Primary:   commonJSON.InterfaceBool(ref["primary"]),
			Extension: strings.TrimSpace(commonJSON.InterfaceString(ref["extension"])),
		})
	}
	return refs
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
	if resolved := normalizeContainerAttributeChildrenForPreview(formatName, children); resolved != nil {
		return resolved
	}
	return resolveContainerAttributeChildrenForPreview(formatName, children)
}

func BuildContainerPreviewFromInfo(info *datatype.ContainerInfo, fallbackFormat string) map[string]interface{} {
	return buildContainerPreviewFromContainerInfo(info, fallbackFormat)
}

func ResolveContainerInfoForPreview(info *datatype.ContainerInfo) *datatype.ContainerInfo {
	return resolveContainerChildrenForPreview(info)
}

func ContainerInfoTruncated(info *datatype.ContainerInfo) bool {
	return containerInfoTruncated(info)
}

func BuildContainerMetadata(info *datatype.ContainerInfo, req *ObjectContentRequest, formatType format.FormatType) map[string]interface{} {
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

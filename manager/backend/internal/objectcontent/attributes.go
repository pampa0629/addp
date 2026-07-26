package objectcontent

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/catalogutil"
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
	return catalogutil.StringAttribute(attrs, key)
}

func interfaceSlice(value interface{}) []interface{} {
	return commonJSON.InterfaceSlice(value)
}

func rawMapAttribute(value interface{}) map[string]interface{} {
	return commonJSON.InterfaceMap(value)
}

func ContainerChildInfoFromMap(child map[string]interface{}) datatype.ContainerChildInfo {
	if child == nil {
		child = map[string]interface{}{}
	}
	name := strings.TrimSpace(commonJSON.InterfaceString(child["name"]))
	childKind := strings.TrimSpace(commonJSON.InterfaceString(child["child_kind"]))
	dataType := datatype.ParseDataType(commonJSON.InterfaceString(child["data_type"]))
	native := containerChildNativeFromMap(child)

	rowCountValue := commonJSON.InterfaceInt64(child["row_count"])
	var rowCount *int64
	if hasCountValue(child, "row_count") && rowCountValue >= 0 {
		rowCount = &rowCountValue
	}
	estimatedRowCountValue := commonJSON.InterfaceInt64(child["estimated_row_count"])
	var estimatedRowCount *int64
	if hasCountValue(child, "estimated_row_count") && estimatedRowCountValue >= 0 {
		estimatedRowCount = &estimatedRowCountValue
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
		Name:              name,
		ChildKind:         childKind,
		DataType:          dataType,
		Format:            canonicalContainerChildFormat(commonJSON.InterfaceString(child["format"])),
		Refs:              containerChildRefsFromMap(child),
		RowCount:          rowCount,
		EstimatedRowCount: estimatedRowCount,
		ColumnCount:       columnCount,
		HasHeader:         hasHeader,
		Native:            native,
	}
}

func hasCountValue(values map[string]interface{}, key string) bool {
	value, ok := values[key]
	if !ok || value == nil {
		return false
	}
	if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
		return false
	}
	return true
}

func canonicalContainerChildFormat(formatName string) string {
	if normalized := format.NormalizeFormat(formatName); normalized != format.FormatUnknown {
		return string(normalized)
	}
	return ""
}

func containerChildNativeFromMap(child map[string]interface{}) map[string]interface{} {
	native := filterContainerChildNativeMap(rawMapAttribute(child["native"]))
	if native == nil {
		native = map[string]interface{}{}
	}
	for key, value := range child {
		if !containerChildNativeKey(key) {
			continue
		}
		native[key] = value
	}
	if len(native) == 0 {
		return nil
	}
	return native
}

func filterContainerChildNativeMap(values map[string]interface{}) map[string]interface{} {
	filtered := map[string]interface{}{}
	for key, value := range values {
		if isContainerChildSchemaProperty(key) {
			continue
		}
		filtered[key] = value
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func containerChildNativeKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "table", "path", "content_type", "uncompressed_size", "compressed_size", "modified_at", "role", "extension", "preview_material", "preview_renderer", "previewable", "ref_preview":
		return true
	default:
		return false
	}
}

func isContainerChildSchemaProperty(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "columns", "fields", "schema", "table_info", "type_info":
		return true
	default:
		return false
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
	return format.NormalizeFormat(formatName)
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

func BuildContainerPreviewFromMetaAttributes(attrs map[string]interface{}, sizeBytes int64) map[string]interface{} {
	return buildContainerPreviewFromMetaAttributes(attrs, sizeBytes)
}

func ResolveContainerAttributeChildrenForPreview(formatName string, children []interface{}) *containerPreviewChildren {
	if resolved := normalizeContainerAttributeChildrenForPreview(formatName, children); resolved != nil {
		return resolved
	}
	return resolveContainerAttributeChildrenForPreview(formatName, children)
}

func BuildContainerPreviewFromInfo(info *datatype.ContainerInfo, fallbackFormat string) map[string]interface{} {
	return buildContainerPreviewFromContainerInfo(info, fallbackFormat, nil)
}

func ResolveContainerInfoForPreview(info *datatype.ContainerInfo) *datatype.ContainerInfo {
	resolved, _ := resolveContainerChildrenForPreview(info)
	return resolved
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

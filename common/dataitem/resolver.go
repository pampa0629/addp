package dataitem

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

// DirectoryResolveInput 是目录级组合形态推断的输入。
type DirectoryResolveInput struct {
	ContentReader plugin.ContentReadableProvider
	ConnInfo      plugin.ConnectionInfo
	EngineID      uint
	DirPath       string
	Files         []plugin.FileEntry
	Subdirs       []plugin.DirEntry
	// RecursiveFiles/RecursiveSubdirs 由扫描入口在需要识别 whole scope 时提供。
	// detector 只消费观察资源，不自行遍历存储引擎。
	RecursiveFiles   []plugin.FileEntry
	RecursiveSubdirs []plugin.DirEntry
}

// SingleResourceInput 是 single 组织方式 item 推断的输入。
type SingleResourceInput struct {
	Name        string
	Path        string
	Size        int64
	ContentType string
	Format      string
}

// ResolveItems 使用已注册 detector 从一个扫描范围内识别 0..N 个数据项。
func ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
	result := &DetectionResult{
		Items:  []*DetectedItem{},
		Claims: ResourceClaimSet{},
	}
	for _, d := range GetAll() {
		detectorInput := input
		detectorInput.Files = unclaimedFiles(input.Files, result.Claims)
		detectorInput.RecursiveFiles = unclaimedFiles(input.RecursiveFiles, result.Claims)
		if len(input.Files) > 0 && len(detectorInput.Files) == 0 &&
			(len(input.RecursiveFiles) == 0 || len(detectorInput.RecursiveFiles) == 0) {
			break
		}
		if scoped, ok := d.(ScopeItemDetector); ok {
			scopeResult, err := scoped.ResolveItems(ctx, detectorInput)
			if err != nil {
				return nil, err
			}
			if scopeResult == nil {
				continue
			}
			for _, item := range scopeResult.Items {
				if item != nil {
					result.Items = append(result.Items, item)
				}
			}
			for path, claimed := range scopeResult.Claims {
				if claimed {
					result.Claims[path] = true
				}
			}
			if scopeResult.Exclusive {
				result.Exclusive = true
				return result, nil
			}
			continue
		}

		if !d.Detect(ctx, detectorInput.Files, detectorInput.Subdirs) {
			continue
		}
		info, err := d.ExtractItemInfo(ctx, detectorInput.ContentReader, detectorInput.ConnInfo, detectorInput.EngineID, detectorInput.DirPath, detectorInput.Files)
		if err != nil {
			return nil, err
		}
		if info == nil {
			info = &CompositeItemInfo{}
		}

		totalSize := sumFileSize(detectorInput.Files)
		if info.SizeBytes != nil {
			totalSize = *info.SizeBytes
		}

		organization := info.Organization
		if organization == "" {
			organization = OrganizationWhole
		}

		dataType := info.DataType
		if dataType == "" {
			dataType = InferDataType(info.Format, "")
		}

		entryPath := info.EntryPath
		if entryPath == "" {
			entryPath = detectorInput.DirPath
		}

		componentFiles := info.ComponentFiles
		if len(componentFiles) == 0 {
			componentFiles = filePaths(detectorInput.Files)
		}

		item := &DetectedItem{
			ItemType:       d.ItemType(),
			Organization:   organization,
			DataType:       dataType,
			Format:         info.Format,
			PhysicalPath:   detectorInput.DirPath,
			EntryPath:      entryPath,
			ComponentFiles: componentFiles,
			SizeBytes:      totalSize,
			Fields:         info.Fields,
			Attributes:     info.Attributes,
		}
		result.Items = append(result.Items, item)
		for _, path := range componentFiles {
			result.Claims[path] = true
		}
		result.Exclusive = organization == OrganizationWhole
		if result.Exclusive {
			return result, nil
		}
	}
	return result, nil
}

func unclaimedFiles(files []plugin.FileEntry, claims ResourceClaimSet) []plugin.FileEntry {
	if len(files) == 0 || len(claims) == 0 {
		return files
	}
	filtered := make([]plugin.FileEntry, 0, len(files))
	for _, file := range files {
		if claims[file.Path] {
			continue
		}
		filtered = append(filtered, file)
	}
	return filtered
}

// BuildAttributes 将 detector 输出和标准 item 语义合并为可落库的 attributes。
func BuildAttributes(item *DetectedItem) map[string]interface{} {
	if item == nil {
		return map[string]interface{}{}
	}
	attrs := make(map[string]interface{}, len(item.Attributes)+10)
	for k, v := range item.Attributes {
		attrs[k] = v
	}

	itemAttrs := map[string]interface{}{}
	storageAttrs := map[string]interface{}{}

	setSectionValue(itemAttrs, "organization", string(item.Organization))
	setSectionValue(itemAttrs, "data_type", string(item.DataType))
	if item.Format != "" {
		setSectionValue(itemAttrs, "format", item.Format)
	}
	if item.PhysicalPath != "" {
		setSectionValue(storageAttrs, "physical_path", item.PhysicalPath)
	}
	if item.Organization == OrganizationMulti && len(item.ComponentFiles) > 0 {
		setSectionValue(itemAttrs, "component_files", item.ComponentFiles)
		setSectionValue(itemAttrs, "file_count", len(item.ComponentFiles))
	}
	if item.Organization == OrganizationWhole {
		setSectionValue(itemAttrs, "scope_exclusive", true)
		setSectionValue(itemAttrs, "claim_policy", "whole_scope")
	}
	if item.SizeBytes > 0 {
		setSectionValue(storageAttrs, "total_size", item.SizeBytes)
	}
	attrs["item"] = mergeSection(attrs["item"], itemAttrs)
	attrs["storage"] = mergeSection(attrs["storage"], storageAttrs)
	return attrs
}

func setSectionValue(section map[string]interface{}, key string, value interface{}) {
	section[key] = value
}

func mergeSection(existing interface{}, additions map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	if section, ok := existing.(map[string]interface{}); ok {
		for k, v := range section {
			merged[k] = v
		}
	}
	for k, v := range additions {
		merged[k] = v
	}
	return merged
}

// InferSingleResourceItem 基于一个资源推断基础 item 语义。
func InferSingleResourceItem(file plugin.FileEntry) *DetectedItem {
	return InferSingleResource(SingleResourceInput{
		Name:        file.Name,
		Path:        file.Path,
		Size:        file.Size,
		ContentType: file.ContentType,
	})
}

// InferSingleResource 基于单个资源信息推断基础 item 语义。
func InferSingleResource(input SingleResourceInput) *DetectedItem {
	formatName := InferFormat(input.Name, input.ContentType, input.Format)
	organization := OrganizationSingle
	dataType := InferDataType(formatName, input.ContentType)
	itemType := "file"
	if rule, ok := MatchBuiltinSingleResourceRule(formatName); ok {
		organization = rule.Organization
		dataType = rule.DataType
		itemType = rule.ItemType
	}
	item := &DetectedItem{
		ItemType:       itemType,
		Organization:   organization,
		DataType:       dataType,
		Format:         formatName,
		PhysicalPath:   input.Path,
		EntryPath:      input.Path,
		ComponentFiles: []string{input.Path},
		SizeBytes:      input.Size,
		Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"path":         input.Path,
				"size":         input.Size,
				"content_type": input.ContentType,
			},
		},
	}
	applyKnownFormatCapabilities(item)
	return item
}

func applyKnownFormatCapabilities(item *DetectedItem) {
	if item == nil {
		return
	}
	switch item.Format {
	case string(format.FormatGeoJSON):
		upsertNestedAttributeMap(item.Attributes, "capabilities", "spatial", map[string]interface{}{
			"geometry_columns": []map[string]interface{}{{
				"name":          "geometry",
				"geometry_type": "geometry",
				"srid":          4326,
			}},
			"primary_geometry_column": "geometry",
			"has_spatial_index":       false,
		})
	case string(format.FormatTIFF):
		upsertNestedAttributeMap(item.Attributes, "capabilities", "spatial", map[string]interface{}{
			"extent":            nil,
			"has_spatial_index": false,
		})
	}
}

func upsertNestedAttributeMap(attrs map[string]interface{}, section string, namespace string, values map[string]interface{}) {
	if attrs == nil || section == "" || namespace == "" || len(values) == 0 {
		return
	}
	sectionAttrs, _ := attrs[section].(map[string]interface{})
	if sectionAttrs == nil {
		sectionAttrs = map[string]interface{}{}
	}
	namespaceAttrs, _ := sectionAttrs[namespace].(map[string]interface{})
	if namespaceAttrs == nil {
		namespaceAttrs = map[string]interface{}{}
	}
	for key, value := range values {
		namespaceAttrs[key] = value
	}
	sectionAttrs[namespace] = namespaceAttrs
	attrs[section] = sectionAttrs
}

// InferFormat 在 item 已经归并后，统一规范化主文件格式。
func InferFormat(fileName, contentType, explicitFormat string) string {
	if explicitFormat != "" {
		if canonical := canonicalFormat(explicitFormat); canonical != "" {
			return canonical
		}
	}
	if formatFromMIME := format.MIMEToFormat(contentType); formatFromMIME != format.FormatUnknown {
		return string(formatFromMIME)
	}
	formatType := format.DetectFormat(fileName, nil)
	return normalizeFormat(string(formatType), fileName)
}

// InferDataType 从规范化格式和 MIME 类型推断主数据类型。
func InferDataType(formatName, contentType string) DataType {
	normalizedFormat := canonicalFormat(formatName)
	switch format.FormatType(normalizedFormat) {
	case format.FormatCSV, format.FormatExcel, format.FormatTSV,
		format.FormatShapefile, format.FormatGeoJSON, format.FormatGeoPackage,
		format.FormatKML, format.FormatKMZ, format.FormatParquet, format.FormatAvro,
		format.FormatSQLite, format.FormatPostgres, format.FormatMySQL,
		format.FormatJSON, format.FormatXML:
		if normalizedFormat == string(format.FormatExcel) ||
			normalizedFormat == string(format.FormatSQLite) ||
			normalizedFormat == string(format.FormatGeoPackage) {
			return DataTypeContainer
		}
		return DataTypeTable
	case format.FormatJPEG, format.FormatPNG, format.FormatGIF, format.FormatTIFF, format.FormatImage:
		return DataTypeMedia
	case format.FormatVideo:
		return DataTypeMedia
	case format.FormatPDF, format.FormatDOCX, format.FormatPPTX, format.FormatWPS, format.FormatText:
		return DataTypeDocument
	case format.FormatAudio:
		return DataTypeMedia
	}

	switch normalizedFormat {
	case "orc", "clickhouse", "doris", "mongodb":
		return DataTypeTable
	}

	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return DataTypeMedia
	case strings.HasPrefix(contentType, "video/"):
		return DataTypeMedia
	case strings.HasPrefix(contentType, "audio/"):
		return DataTypeMedia
	case contentType == "application/pdf", strings.HasPrefix(contentType, "text/"):
		return DataTypeDocument
	default:
		return DataTypeUnknown
	}
}

func sumFileSize(files []plugin.FileEntry) int64 {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return total
}

func filePaths(files []plugin.FileEntry) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		if f.Path != "" {
			paths = append(paths, f.Path)
		}
	}
	return paths
}

func normalizeFormat(formatName, fileName string) string {
	if canonical := canonicalFormat(formatName); canonical != "" {
		return canonical
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(fileName)), ".")
	if ext == "" {
		return string(format.FormatUnknown)
	}
	if canonical := canonicalFormat(ext); canonical != "" {
		return canonical
	}
	return strings.ToLower(strings.TrimSpace(ext))
}

func canonicalFormat(formatName string) string {
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(formatName)), ".")
	if name == "" || name == string(format.FormatUnknown) {
		return ""
	}
	switch name {
	case "shp", "shx", "dbf", "prj", "cpg", "sbn", "sbx":
		return string(format.FormatShapefile)
	case "geojson":
		return string(format.FormatGeoJSON)
	case "gpkg":
		return string(format.FormatGeoPackage)
	case "xls", "xlsx":
		return string(format.FormatExcel)
	case "jpg", "jpeg":
		return string(format.FormatJPEG)
	case "tif", "tiff":
		return string(format.FormatTIFF)
	case "txt":
		return string(format.FormatText)
	case "sqlite3", "db":
		return string(format.FormatSQLite)
	default:
		return name
	}
}

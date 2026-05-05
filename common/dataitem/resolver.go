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
	// RecursiveFiles/RecursiveSubdirs 由扫描入口在需要识别 directory_tree 时提供。
	// detector 只消费观察资源，不自行遍历存储引擎。
	RecursiveFiles   []plugin.FileEntry
	RecursiveSubdirs []plugin.DirEntry
}

// SingleFileInput 是单文件 item 推断的输入。
type SingleFileInput struct {
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

		compositionType := info.CompositionType
		if compositionType == "" {
			compositionType = CompositionTypeDirectoryTree
		}

		dataFamily := info.DataFamily
		if dataFamily == "" {
			dataFamily = InferDataFamily(info.Format, "")
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
			ItemType:        d.ItemType(),
			CompositionType: compositionType,
			DataFamily:      dataFamily,
			Format:          info.Format,
			PhysicalPath:    detectorInput.DirPath,
			EntryPath:       entryPath,
			ComponentFiles:  componentFiles,
			SizeBytes:       totalSize,
			Fields:          info.Fields,
			Attributes:      info.Attributes,
		}
		result.Items = append(result.Items, item)
		for _, path := range componentFiles {
			result.Claims[path] = true
		}
		result.Exclusive = compositionType == CompositionTypeDirectoryTree
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

// ResolveDirectory 保留旧调用入口，返回第一个识别出的 item。
// 新扫描流程应使用 ResolveItems，避免一个扫描范围只能产出一个 item。
func ResolveDirectory(ctx context.Context, input DirectoryResolveInput) (*DetectedItem, error) {
	result, err := ResolveItems(ctx, input)
	if err != nil || result == nil || len(result.Items) == 0 {
		return nil, err
	}
	return result.Items[0], nil
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

	setSectionValue(itemAttrs, "composition_type", string(item.CompositionType))
	setSectionValue(itemAttrs, "data_family", string(item.DataFamily))
	if item.Format != "" {
		setSectionValue(itemAttrs, "format", item.Format)
	}
	if item.PhysicalPath != "" {
		setSectionValue(storageAttrs, "physical_path", item.PhysicalPath)
	}
	if item.EntryPath != "" {
		setSectionValue(itemAttrs, "entry_path", item.EntryPath)
	}
	if len(item.ComponentFiles) > 0 {
		setSectionValue(itemAttrs, "component_files", item.ComponentFiles)
		setSectionValue(itemAttrs, "file_count", len(item.ComponentFiles))
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

// InferSingleFileItem 基于单个文件推断基础 item 语义。
func InferSingleFileItem(file plugin.FileEntry) *DetectedItem {
	return InferSingleFile(SingleFileInput{
		Name:        file.Name,
		Path:        file.Path,
		Size:        file.Size,
		ContentType: file.ContentType,
	})
}

// InferSingleFile 基于单文件信息推断基础 item 语义。
func InferSingleFile(input SingleFileInput) *DetectedItem {
	formatName := InferFormat(input.Name, input.ContentType, input.Format)
	compositionType := CompositionTypeSingleFile
	dataFamily := InferDataFamily(formatName, input.ContentType)
	itemType := "file"
	if rule, ok := MatchBuiltinSingleFileRule(formatName); ok {
		compositionType = rule.CompositionType
		dataFamily = rule.DataFamily
		itemType = rule.ItemType
	}
	return &DetectedItem{
		ItemType:        itemType,
		CompositionType: compositionType,
		DataFamily:      dataFamily,
		Format:          formatName,
		PhysicalPath:    input.Path,
		EntryPath:       input.Path,
		ComponentFiles:  []string{input.Path},
		SizeBytes:       input.Size,
		Attributes: map[string]interface{}{
			"path":         input.Path,
			"size":         input.Size,
			"content_type": input.ContentType,
		},
	}
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

// InferDataFamily 从规范化格式和 MIME 类型推断主数据家族。
func InferDataFamily(formatName, contentType string) DataFamily {
	normalizedFormat := canonicalFormat(formatName)
	switch format.FormatType(normalizedFormat) {
	case format.FormatCSV, format.FormatExcel, format.FormatTSV,
		format.FormatShapefile, format.FormatGeoJSON, format.FormatGeoPackage,
		format.FormatKML, format.FormatKMZ, format.FormatParquet, format.FormatAvro,
		format.FormatSQLite, format.FormatPostgres, format.FormatMySQL,
		format.FormatJSON, format.FormatXML:
		return DataFamilyTabular
	case format.FormatJPEG, format.FormatPNG, format.FormatGIF, format.FormatTIFF, format.FormatImage:
		return DataFamilyImage
	case format.FormatVideo:
		return DataFamilyVideo
	case format.FormatPDF, format.FormatDOCX, format.FormatPPTX, format.FormatWPS, format.FormatText:
		return DataFamilyDocument
	case format.FormatAudio:
		return DataFamilyAudio
	}

	switch normalizedFormat {
	case "orc", "clickhouse", "doris", "mongodb":
		return DataFamilyTabular
	}

	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return DataFamilyImage
	case strings.HasPrefix(contentType, "video/"):
		return DataFamilyVideo
	case strings.HasPrefix(contentType, "audio/"):
		return DataFamilyAudio
	case contentType == "application/pdf", strings.HasPrefix(contentType, "text/"):
		return DataFamilyDocument
	default:
		return DataFamilyUnknown
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

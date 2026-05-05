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
}

// SingleFileInput 是单文件 item 推断的输入。
type SingleFileInput struct {
	Name        string
	Path        string
	Size        int64
	ContentType string
	Format      string
}

// ResolveDirectory 使用已注册 detector 推断一个目录是否整体构成数据项。
func ResolveDirectory(ctx context.Context, input DirectoryResolveInput) (*DetectedItem, error) {
	for _, d := range GetAll() {
		if !d.Detect(ctx, input.Files, input.Subdirs) {
			continue
		}
		info, err := d.ExtractItemInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.DirPath, input.Files)
		if err != nil {
			return nil, err
		}
		if info == nil {
			info = &CompositeItemInfo{}
		}

		totalSize := sumFileSize(input.Files)
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
			entryPath = input.DirPath
		}

		componentFiles := info.ComponentFiles
		if len(componentFiles) == 0 {
			componentFiles = filePaths(input.Files)
		}

		return &DetectedItem{
			ItemType:        d.ItemType(),
			CompositionType: compositionType,
			DataFamily:      dataFamily,
			Format:          info.Format,
			PhysicalPath:    input.DirPath,
			EntryPath:       entryPath,
			ComponentFiles:  componentFiles,
			SizeBytes:       totalSize,
			Fields:          info.Fields,
			Attributes:      info.Attributes,
		}, nil
	}
	return nil, nil
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

	setCompatAndSectionValue(attrs, itemAttrs, "composition_type", string(item.CompositionType))
	setCompatAndSectionValue(attrs, itemAttrs, "data_family", string(item.DataFamily))
	if item.Format != "" {
		setCompatAndSectionValue(attrs, itemAttrs, "format", item.Format)
	}
	if item.PhysicalPath != "" {
		setCompatAndSectionValue(attrs, storageAttrs, "physical_path", item.PhysicalPath)
	}
	if item.EntryPath != "" {
		setCompatAndSectionValue(attrs, itemAttrs, "entry_path", item.EntryPath)
	}
	if len(item.ComponentFiles) > 0 {
		setCompatAndSectionValue(attrs, itemAttrs, "component_files", item.ComponentFiles)
		setCompatAndSectionValue(attrs, itemAttrs, "file_count", len(item.ComponentFiles))
	}
	if item.SizeBytes > 0 {
		setCompatAndSectionValue(attrs, storageAttrs, "total_size", item.SizeBytes)
	}
	attrs["item"] = mergeSection(attrs["item"], itemAttrs)
	attrs["storage"] = mergeSection(attrs["storage"], storageAttrs)
	return attrs
}

func setCompatAndSectionValue(attrs map[string]interface{}, section map[string]interface{}, key string, value interface{}) {
	attrs[key] = value
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
	if formatName == string(format.FormatSQLite) || formatName == string(format.FormatGeoPackage) {
		compositionType = CompositionTypeContainerFile
	}
	return &DetectedItem{
		CompositionType: compositionType,
		DataFamily:      InferDataFamily(formatName, input.ContentType),
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

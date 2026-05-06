package dataitem

import (
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

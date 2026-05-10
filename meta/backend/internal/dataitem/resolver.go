package dataitem

import (
	"path/filepath"
	"strings"

	"github.com/addp/common/format"
)

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
	if capability, ok := format.GetFormatCapability(format.FormatType(normalizedFormat)); ok {
		if dataType := dataTypeFromFormatCapability(capability.DataType); dataType != "" {
			return dataType
		}
	}
	switch format.FormatType(normalizedFormat) {
	case format.FormatCSV, format.FormatExcel, format.FormatTSV,
		format.FormatShapefile, format.FormatGeoPackage,
		format.FormatKML, format.FormatKMZ, format.FormatParquet, format.FormatAvro,
		format.FormatSQLite, format.FormatPostgres, format.FormatMySQL,
		format.FormatXML:
		if normalizedFormat == string(format.FormatExcel) ||
			normalizedFormat == string(format.FormatSQLite) ||
			normalizedFormat == string(format.FormatGeoPackage) {
			return DataTypeContainer
		}
		return DataTypeTable
	case format.FormatJSON:
		return DataTypeDocument
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

func dataTypeFromFormatCapability(dataType string) DataType {
	switch DataType(strings.ToLower(strings.TrimSpace(dataType))) {
	case DataTypeTable:
		return DataTypeTable
	case DataTypeDocument:
		return DataTypeDocument
	case DataTypeMedia:
		return DataTypeMedia
	case DataTypeContainer:
		return DataTypeContainer
	case DataTypeGraph:
		return DataTypeGraph
	case DataTypeUnknown:
		return DataTypeUnknown
	default:
		return ""
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
		return string(format.FormatJSON)
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

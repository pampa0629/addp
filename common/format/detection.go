package format

import (
	"bytes"
	"mime"
	"path/filepath"
	"strings"
)

// FormatType 标准化的格式类型
type FormatType string

const (
	// 地理空间格式
	FormatShapefile  FormatType = "shapefile"
	FormatGeoPackage FormatType = "geopackage"
	FormatKML        FormatType = "kml"
	FormatKMZ        FormatType = "kmz"

	// 表格格式
	FormatCSV   FormatType = "csv"
	FormatExcel FormatType = "excel" // .xlsx, .xls
	FormatTSV   FormatType = "tsv"

	// 文档格式
	FormatPDF      FormatType = "pdf"
	FormatDOCX     FormatType = "docx"
	FormatPPTX     FormatType = "pptx"
	FormatWPS      FormatType = "wps"
	FormatText     FormatType = "text"
	FormatMarkdown FormatType = "markdown"

	// 图像格式
	FormatImage FormatType = "image" // 通用图像
	FormatJPEG  FormatType = "jpeg"
	FormatPNG   FormatType = "png"
	FormatGIF   FormatType = "gif"
	FormatTIFF  FormatType = "tiff"
	FormatWebP  FormatType = "webp"
	FormatBMP   FormatType = "bmp"
	FormatSVG   FormatType = "svg"
	FormatAVIF  FormatType = "avif"
	FormatHEIC  FormatType = "heic"

	// 数据库格式
	FormatSQLite   FormatType = "sqlite"
	FormatPostgres FormatType = "postgres"
	FormatMySQL    FormatType = "mysql"

	// 数据交换格式
	FormatJSON    FormatType = "json"
	FormatXML     FormatType = "xml"
	FormatParquet FormatType = "parquet"
	FormatORC     FormatType = "orc"
	FormatAvro    FormatType = "avro"

	// 多媒体格式
	FormatVideo FormatType = "video"
	FormatAudio FormatType = "audio"
	FormatMP4   FormatType = "mp4"
	FormatMOV   FormatType = "mov"
	FormatMKV   FormatType = "mkv"
	FormatAVI   FormatType = "avi"
	FormatWebM  FormatType = "webm"
	FormatMP3   FormatType = "mp3"
	FormatWAV   FormatType = "wav"
	FormatFLAC  FormatType = "flac"
	FormatAAC   FormatType = "aac"
	FormatOGG   FormatType = "ogg"

	// 未知格式
	FormatUnknown FormatType = "unknown"
)

// DetectFormat 根据文件名和内容前缀检测格式
// peek: 文件的前512字节（用于Magic Bytes检测）
func DetectFormat(filename string, peek []byte) FormatType {
	// 1. 先根据文件扩展名快速判断
	ext := strings.ToLower(filepath.Ext(filename))
	if format := extToFormat(ext); format != FormatUnknown {
		// 对于某些格式，需要进一步验证Magic Bytes
		if needMagicValidation(format) && len(peek) > 0 {
			if !validateMagicBytes(format, peek) {
				return FormatUnknown
			}
		}
		return format
	}

	// 2. 如果扩展名无法判断，尝试Magic Bytes检测
	if len(peek) > 0 {
		if format := detectByMagic(peek); format != FormatUnknown {
			return format
		}
	}

	// 3. 特殊处理：Shapefile组合文件
	if isShapefileComponent(filename) {
		return FormatShapefile
	}

	return FormatUnknown
}

// extToFormat 根据文件扩展名返回格式类型
func extToFormat(ext string) FormatType {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if formatType := descriptorFormatByExtension(ext); formatType != FormatUnknown {
		return formatType
	}

	switch ext {
	// 地理空间
	case ".shp":
		return FormatShapefile
	case ".gpkg":
		return FormatGeoPackage
	case ".kml":
		return FormatKML
	case ".kmz":
		return FormatKMZ

	// 表格
	case ".csv":
		return FormatCSV
	case ".xlsx":
		return FormatExcel
	case ".xls":
		return FormatExcel
	case ".tsv":
		return FormatTSV

	// 文档
	case ".pdf":
		return FormatPDF
	case ".docx":
		return FormatDOCX
	case ".pptx":
		return FormatPPTX
	case ".wps":
		return FormatWPS
	case ".txt":
		return FormatText
	case ".md", ".markdown":
		return FormatMarkdown

	// 图像
	case ".jpg", ".jpeg":
		return FormatJPEG
	case ".png":
		return FormatPNG
	case ".gif":
		return FormatGIF
	case ".tif", ".tiff":
		return FormatTIFF
	case ".webp":
		return FormatWebP
	case ".bmp":
		return FormatBMP
	case ".svg", ".svgz":
		return FormatSVG
	case ".avif":
		return FormatAVIF
	case ".heic", ".heif":
		return FormatHEIC

	// 数据库
	case ".sqlite", ".db", ".sqlite3":
		return FormatSQLite

	// 数据交换
	case ".json", ".geojson":
		return FormatJSON
	case ".xml":
		return FormatXML
	case ".parquet":
		return FormatParquet
	case ".orc":
		return FormatORC
	case ".avro":
		return FormatAvro

	// 多媒体
	case ".mp4", ".m4v":
		return FormatMP4
	case ".avi":
		return FormatAVI
	case ".mov", ".qt":
		return FormatMOV
	case ".mkv":
		return FormatMKV
	case ".webm":
		return FormatWebM
	case ".mp3":
		return FormatMP3
	case ".wav":
		return FormatWAV
	case ".flac":
		return FormatFLAC
	case ".aac", ".m4a":
		return FormatAAC
	case ".ogg", ".oga", ".opus":
		return FormatOGG
	case ".flv", ".wmv":
		return FormatVideo

	default:
		return FormatUnknown
	}
}

// needMagicValidation 判断某个格式是否需要Magic Bytes验证
func needMagicValidation(format FormatType) bool {
	switch format {
	case FormatPDF, FormatSQLite, FormatGeoPackage, FormatJPEG, FormatPNG, FormatGIF:
		return true
	default:
		return false
	}
}

// validateMagicBytes 验证文件的Magic Bytes是否匹配格式
func validateMagicBytes(format FormatType, peek []byte) bool {
	magic := getMagicBytes(format)
	if len(magic) == 0 || len(peek) < len(magic) {
		return true // 无法验证，默认信任扩展名
	}
	return bytes.HasPrefix(peek, magic)
}

// getMagicBytes 返回格式的Magic Bytes
func getMagicBytes(format FormatType) []byte {
	switch format {
	case FormatPDF:
		return []byte("%PDF")
	case FormatSQLite, FormatGeoPackage:
		return []byte("SQLite format 3")
	case FormatJPEG:
		return []byte{0xFF, 0xD8, 0xFF}
	case FormatPNG:
		return []byte{0x89, 0x50, 0x4E, 0x47}
	case FormatGIF:
		return []byte("GIF89a")
	default:
		return nil
	}
}

// detectByMagic 通过Magic Bytes检测格式
func detectByMagic(peek []byte) FormatType {
	lowerPeek := bytes.ToLower(bytes.TrimSpace(peek))
	if bytes.HasPrefix(lowerPeek, []byte("<svg")) ||
		(bytes.HasPrefix(lowerPeek, []byte("<?xml")) && bytes.Contains(lowerPeek[:minInt(len(lowerPeek), 4096)], []byte("<svg"))) {
		return FormatSVG
	}

	if len(peek) >= 12 && bytes.HasPrefix(peek, []byte("RIFF")) {
		switch string(peek[8:12]) {
		case "WEBP":
			return FormatWebP
		case "WAVE":
			return FormatWAV
		case "AVI ":
			return FormatAVI
		}
	}
	if len(peek) >= 12 && string(peek[4:8]) == "ftyp" {
		brand := strings.ToLower(string(peek[8:minInt(len(peek), 12)]))
		switch brand {
		case "avif":
			return FormatAVIF
		case "heic", "heix", "hevc", "hevx", "mif1", "msf1":
			return FormatHEIC
		case "qt  ":
			return FormatMOV
		default:
			return FormatMP4
		}
	}

	magicMap := map[string]FormatType{
		"%PDF":             FormatPDF,
		"SQLite format 3":  FormatSQLite,
		"\xFF\xD8\xFF":     FormatJPEG,
		"\x89PNG":          FormatPNG,
		"GIF89a":           FormatGIF,
		"GIF87a":           FormatGIF,
		"BM":               FormatBMP,
		"ID3":              FormatMP3,
		"OggS":             FormatOGG,
		"fLaC":             FormatFLAC,
		"\x1A\x45\xDF\xA3": FormatMKV,
		"PK\x03\x04":       FormatUnknown, // ZIP格式，可能是DOCX/XLSX/GPKG等
		"0000":             FormatShapefile,
		"9994":             FormatShapefile,
	}

	for magic, format := range magicMap {
		if bytes.HasPrefix(peek, []byte(magic)) {
			return format
		}
	}

	return FormatUnknown
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isShapefileComponent 判断是否为Shapefile组合文件的一部分
func isShapefileComponent(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".shp", ".shx", ".dbf", ".prj", ".sbn", ".sbx", ".cpg":
		return true
	default:
		return false
	}
}

// MIMEToFormat 将MIME类型转换为标准格式类型
func MIMEToFormat(mimeType string) FormatType {
	// 规范化MIME类型
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))

	// 移除参数（如 "text/plain; charset=utf-8" -> "text/plain"）
	if idx := strings.Index(mimeType, ";"); idx > 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	if formatType := descriptorFormatByMIME(mimeType); formatType != FormatUnknown {
		return formatType
	}

	switch mimeType {
	// 地理空间
	case "application/geo+json", "application/vnd.geo+json":
		return FormatJSON
	case "application/geopackage+sqlite3":
		return FormatGeoPackage
	case "application/vnd.google-earth.kml+xml":
		return FormatKML
	case "application/vnd.google-earth.kmz":
		return FormatKMZ

	// 表格
	case "text/csv":
		return FormatCSV
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return FormatExcel
	case "application/vnd.ms-excel":
		return FormatExcel
	case "text/tab-separated-values":
		return FormatTSV

	// 文档
	case "application/pdf":
		return FormatPDF
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return FormatDOCX
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return FormatPPTX
	case "application/vnd.ms-works", "application/wps-office.doc", "application/x-wps", "application/kswps":
		return FormatWPS
	case "text/plain":
		return FormatText
	case "text/markdown", "text/x-markdown":
		return FormatMarkdown

	// 图像
	case "image/jpeg":
		return FormatJPEG
	case "image/png":
		return FormatPNG
	case "image/gif":
		return FormatGIF
	case "image/tiff":
		return FormatTIFF

	// 数据库
	case "application/x-sqlite3", "application/vnd.sqlite3":
		return FormatSQLite

	// 数据交换
	case "application/json":
		return FormatJSON
	case "application/xml", "text/xml":
		return FormatXML
	case "application/x-parquet", "application/parquet", "application/vnd.apache.parquet":
		return FormatParquet
	case "application/orc", "application/vnd.apache.orc":
		return FormatORC
	case "application/avro":
		return FormatAvro

	// 多媒体
	case "video/mp4", "application/mp4":
		return FormatMP4
	case "video/x-msvideo", "video/avi":
		return FormatAVI
	case "video/quicktime":
		return FormatMOV
	case "video/x-matroska", "video/matroska":
		return FormatMKV
	case "video/webm":
		return FormatWebM
	case "audio/mpeg", "audio/mp3":
		return FormatMP3
	case "audio/wav", "audio/wave", "audio/x-wav":
		return FormatWAV
	case "audio/flac":
		return FormatFLAC
	case "audio/aac", "audio/mp4", "audio/x-m4a":
		return FormatAAC
	case "audio/ogg", "audio/opus":
		return FormatOGG

	default:
		// 尝试主类型匹配
		if strings.HasPrefix(mimeType, "image/") {
			return FormatImage
		}
		if strings.HasPrefix(mimeType, "video/") {
			return FormatVideo
		}
		if strings.HasPrefix(mimeType, "audio/") {
			return FormatAudio
		}
		return FormatUnknown
	}
}

// FormatToMIME 将格式类型转换为主要MIME类型
func FormatToMIME(format FormatType) string {
	if descriptor, ok := GetFormatDescriptor(format); ok && len(descriptor.Identification.MimeTypes) > 0 {
		return descriptor.Identification.MimeTypes[0]
	}

	switch format {
	// 地理空间
	case FormatGeoPackage:
		return "application/geopackage+sqlite3"
	case FormatKML:
		return "application/vnd.google-earth.kml+xml"
	case FormatKMZ:
		return "application/vnd.google-earth.kmz"
	case FormatShapefile:
		return "application/x-shapefile" // 非标准

	// 表格
	case FormatCSV:
		return "text/csv"
	case FormatExcel:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case FormatTSV:
		return "text/tab-separated-values"

	// 文档
	case FormatPDF:
		return "application/pdf"
	case FormatDOCX:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case FormatPPTX:
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case FormatWPS:
		return "application/vnd.ms-works"
	case FormatText:
		return "text/plain"
	case FormatMarkdown:
		return "text/markdown"

	// 图像
	case FormatJPEG:
		return "image/jpeg"
	case FormatPNG:
		return "image/png"
	case FormatGIF:
		return "image/gif"
	case FormatTIFF:
		return "image/tiff"
	case FormatImage:
		return "image/*"

	// 数据库
	case FormatSQLite:
		return "application/x-sqlite3"

	// 数据交换
	case FormatJSON:
		return "application/json"
	case FormatXML:
		return "application/xml"
	case FormatParquet:
		return "application/x-parquet"
	case FormatORC:
		return "application/vnd.apache.orc"
	case FormatAvro:
		return "application/avro"

	// 多媒体
	case FormatMP4:
		return "video/mp4"
	case FormatMOV:
		return "video/quicktime"
	case FormatMKV:
		return "video/x-matroska"
	case FormatAVI:
		return "video/x-msvideo"
	case FormatWebM:
		return "video/webm"
	case FormatVideo:
		return "video/*"
	case FormatMP3:
		return "audio/mpeg"
	case FormatWAV:
		return "audio/wav"
	case FormatFLAC:
		return "audio/flac"
	case FormatAAC:
		return "audio/aac"
	case FormatOGG:
		return "audio/ogg"
	case FormatAudio:
		return "audio/*"

	default:
		return "application/octet-stream"
	}
}

func descriptorFormatByExtension(ext string) FormatType {
	if ext == "" {
		return FormatUnknown
	}
	for _, descriptor := range ListFormatDescriptors() {
		for _, candidate := range descriptor.Identification.Extensions {
			if strings.EqualFold(candidate, ext) {
				return descriptor.Format
			}
		}
	}
	return FormatUnknown
}

func descriptorFormatByMIME(mimeType string) FormatType {
	if mimeType == "" {
		return FormatUnknown
	}
	for _, descriptor := range ListFormatDescriptors() {
		for _, candidate := range descriptor.Identification.MimeTypes {
			if strings.EqualFold(candidate, mimeType) {
				return descriptor.Format
			}
		}
	}
	return FormatUnknown
}

// GuessContentType 结合文件名和内容猜测MIME类型
// 这是对 mime.TypeByExtension 的增强版本
func GuessContentType(filename string, peek []byte) string {
	// 1. 先使用标准库的 mime.TypeByExtension
	ext := filepath.Ext(filename)
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}

	// 2. 使用我们的格式检测
	format := DetectFormat(filename, peek)
	return FormatToMIME(format)
}

// IsGeospatialFormat 判断是否为地理空间格式
func IsGeospatialFormat(format FormatType) bool {
	if capability, ok := GetFormatCapability(format); ok {
		return capability.Spatial
	}
	switch format {
	case FormatShapefile, FormatGeoPackage, FormatKML, FormatKMZ:
		return true
	default:
		return false
	}
}

// IsDocumentFormat 判断是否为文档格式
func IsDocumentFormat(format FormatType) bool {
	if capability, ok := GetFormatCapability(format); ok {
		return capability.DataType == FormatDataTypeDocument
	}
	switch format {
	case FormatPDF, FormatDOCX, FormatPPTX, FormatWPS, FormatText, FormatMarkdown:
		return true
	default:
		return false
	}
}

// IsImageFormat 判断是否为图像格式
func IsImageFormat(format FormatType) bool {
	switch format {
	case FormatImage, FormatJPEG, FormatPNG, FormatGIF, FormatTIFF, FormatWebP, FormatBMP, FormatSVG, FormatAVIF, FormatHEIC:
		return true
	default:
		return false
	}
}

// IsTableFormat 判断是否为表格格式
func IsTableFormat(format FormatType) bool {
	if capability, ok := GetFormatCapability(format); ok {
		if capability.DataType == FormatDataTypeTable {
			return true
		}
	}
	switch format {
	case FormatCSV, FormatExcel, FormatTSV:
		return true
	default:
		return false
	}
}

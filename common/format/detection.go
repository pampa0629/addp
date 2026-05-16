package format

import (
	"path/filepath"
	"strings"
)

// DetectFormat 根据文件名和内容前缀检测格式。
func DetectFormat(filename string, peek []byte) FormatType {
	ext := strings.ToLower(filepath.Ext(filename))
	if format := extToFormat(ext); format != FormatUnknown {
		if needMagicValidation(format) && len(peek) > 0 && !validateMagicBytes(format, peek) {
			return FormatUnknown
		}
		return format
	}
	if len(peek) > 0 {
		return detectByMagic(peek)
	}
	return FormatUnknown
}

func extToFormat(ext string) FormatType {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if formatType := descriptorFormatByExtension(ext); formatType != FormatUnknown {
		return formatType
	}

	switch ext {
	case ".shp":
		return FormatShapefile
	case ".gpkg":
		return FormatGeoPackage
	case ".kml":
		return FormatKML
	case ".kmz":
		return FormatKMZ
	case ".csv":
		return FormatCSV
	case ".xlsx", ".xls":
		return FormatExcel
	case ".tsv":
		return FormatTSV
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
	case ".sqlite", ".db", ".sqlite3":
		return FormatSQLite
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

func IsImageFormat(format FormatType) bool {
	switch format {
	case FormatImage, FormatJPEG, FormatPNG, FormatGIF, FormatTIFF, FormatWebP, FormatBMP, FormatSVG, FormatAVIF, FormatHEIC:
		return true
	default:
		return false
	}
}

func IsTableFormat(format FormatType) bool {
	if capability, ok := GetFormatCapability(format); ok && capability.DataType == FormatDataTypeTable {
		return true
	}
	switch format {
	case FormatCSV, FormatExcel, FormatTSV:
		return true
	default:
		return false
	}
}

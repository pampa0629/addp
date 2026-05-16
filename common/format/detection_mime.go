package format

import (
	"mime"
	"path/filepath"
	"strings"
)

// MIMEToFormat 将 MIME 类型转换为标准格式类型。
func MIMEToFormat(mimeType string) FormatType {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if idx := strings.Index(mimeType, ";"); idx > 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	if formatType := descriptorFormatByMIME(mimeType); formatType != FormatUnknown {
		return formatType
	}

	switch mimeType {
	case "application/geo+json", "application/vnd.geo+json":
		return FormatJSON
	case "application/geopackage+sqlite3":
		return FormatGeoPackage
	case "application/vnd.google-earth.kml+xml":
		return FormatKML
	case "application/vnd.google-earth.kmz":
		return FormatKMZ
	case "text/csv":
		return FormatCSV
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.ms-excel":
		return FormatExcel
	case "text/tab-separated-values":
		return FormatTSV
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
	case "image/jpeg":
		return FormatJPEG
	case "image/png":
		return FormatPNG
	case "image/gif":
		return FormatGIF
	case "image/tiff":
		return FormatTIFF
	case "application/x-sqlite3", "application/vnd.sqlite3":
		return FormatSQLite
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

// FormatToMIME 将格式类型转换为主要 MIME 类型。
func FormatToMIME(format FormatType) string {
	if descriptor, ok := GetFormatDescriptor(format); ok && len(descriptor.Identification.MimeTypes) > 0 {
		return descriptor.Identification.MimeTypes[0]
	}

	switch format {
	case FormatGeoPackage:
		return "application/geopackage+sqlite3"
	case FormatKML:
		return "application/vnd.google-earth.kml+xml"
	case FormatKMZ:
		return "application/vnd.google-earth.kmz"
	case FormatShapefile:
		return "application/x-shapefile"
	case FormatCSV:
		return "text/csv"
	case FormatExcel:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case FormatTSV:
		return "text/tab-separated-values"
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
	case FormatSQLite:
		return "application/x-sqlite3"
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

// GuessContentType 结合文件名和内容猜测 MIME 类型。
func GuessContentType(filename string, peek []byte) string {
	ext := filepath.Ext(filename)
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}
	return FormatToMIME(DetectFormat(filename, peek))
}

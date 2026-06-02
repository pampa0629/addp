package format

import (
	"strings"

	"github.com/addp/common/datatype"
)

type fallbackIdentification struct {
	format           FormatType
	primaryExtension string
	primaryMIME      string
	dataType         datatype.DataType
}

var fallbackIdentifications = []fallbackIdentification{
	{FormatShapefile, ".shp", "application/x-shapefile", datatype.DataTypeTable},
	{FormatGeoPackage, ".gpkg", "application/geopackage+sqlite3", datatype.DataTypeContainer},
	{FormatKML, ".kml", "application/vnd.google-earth.kml+xml", datatype.DataTypeDocument},
	{FormatKMZ, ".kmz", "application/vnd.google-earth.kmz", datatype.DataTypeContainer},
	{FormatCSV, ".csv", "text/csv", datatype.DataTypeTable},
	{FormatExcel, ".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", datatype.DataTypeContainer},
	{FormatTSV, ".tsv", "text/tab-separated-values", datatype.DataTypeTable},
	{FormatPDF, ".pdf", "application/pdf", datatype.DataTypeDocument},
	{FormatDOCX, ".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", datatype.DataTypeDocument},
	{FormatPPTX, ".pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", datatype.DataTypeDocument},
	{FormatWPS, ".wps", "application/vnd.ms-works", datatype.DataTypeDocument},
	{FormatText, ".txt", "text/plain", datatype.DataTypeDocument},
	{FormatMarkdown, ".md", "text/markdown", datatype.DataTypeDocument},
	{FormatJPEG, ".jpg", "image/jpeg", datatype.DataTypeMedia},
	{FormatPNG, ".png", "image/png", datatype.DataTypeMedia},
	{FormatGIF, ".gif", "image/gif", datatype.DataTypeMedia},
	{FormatTIFF, ".tif", "image/tiff", datatype.DataTypeMedia},
	{FormatWebP, ".webp", "image/webp", datatype.DataTypeMedia},
	{FormatBMP, ".bmp", "image/bmp", datatype.DataTypeMedia},
	{FormatSVG, ".svg", "image/svg+xml", datatype.DataTypeMedia},
	{FormatAVIF, ".avif", "image/avif", datatype.DataTypeMedia},
	{FormatHEIC, ".heic", "image/heic", datatype.DataTypeMedia},
	{FormatSQLite, ".sqlite", "application/x-sqlite3", datatype.DataTypeContainer},
	{FormatJSON, ".json", "application/json", datatype.DataTypeTable},
	{FormatXML, ".xml", "application/xml", datatype.DataTypeDocument},
	{FormatParquet, ".parquet", "application/parquet", datatype.DataTypeTable},
	{FormatORC, ".orc", "application/orc", datatype.DataTypeTable},
	{FormatAvro, ".avro", "application/avro", datatype.DataTypeTable},
	{FormatZIP, ".zip", "application/zip", datatype.DataTypeContainer},
	{FormatMP4, ".mp4", "video/mp4", datatype.DataTypeMedia},
	{FormatAVI, ".avi", "video/x-msvideo", datatype.DataTypeMedia},
	{FormatMOV, ".mov", "video/quicktime", datatype.DataTypeMedia},
	{FormatMKV, ".mkv", "video/x-matroska", datatype.DataTypeMedia},
	{FormatWebM, ".webm", "video/webm", datatype.DataTypeMedia},
	{FormatMP3, ".mp3", "audio/mpeg", datatype.DataTypeMedia},
	{FormatWAV, ".wav", "audio/wav", datatype.DataTypeMedia},
	{FormatFLAC, ".flac", "audio/flac", datatype.DataTypeMedia},
	{FormatAAC, ".aac", "audio/aac", datatype.DataTypeMedia},
	{FormatOGG, ".ogg", "audio/ogg", datatype.DataTypeMedia},
	{FormatVideo, "", "video/*", datatype.DataTypeMedia},
	{FormatAudio, "", "audio/*", datatype.DataTypeMedia},
	{FormatImage, "", "image/*", datatype.DataTypeMedia},
}

// fallbackFormatByExtension keeps detection minimally usable before builtin
// descriptors are imported. It intentionally stores only primary bootstrap
// identifiers; complete identification facts belong to FormatDescriptor.
func fallbackFormatByExtension(ext string) FormatType {
	if ext == "" {
		return FormatUnknown
	}
	for _, identification := range fallbackIdentifications {
		if identification.primaryExtension == ext {
			return identification.format
		}
	}
	return FormatUnknown
}

// fallbackFormatByMIME keeps detection minimally usable before builtin
// descriptors are imported. It intentionally stores only primary bootstrap
// identifiers; complete identification facts belong to FormatDescriptor.
func fallbackFormatByMIME(mimeType string) FormatType {
	for _, identification := range fallbackIdentifications {
		candidate := identification.primaryMIME
		if candidate == mimeType || (strings.HasSuffix(candidate, "/*") && strings.HasPrefix(mimeType, strings.TrimSuffix(candidate, "*"))) {
			return identification.format
		}
	}
	return FormatUnknown
}

func fallbackMIMEForFormat(formatType FormatType) (string, bool) {
	for _, identification := range fallbackIdentifications {
		if identification.format == formatType && identification.primaryMIME != "" {
			return identification.primaryMIME, true
		}
	}
	return "", false
}

func fallbackDataTypeForFormat(formatType FormatType) (datatype.DataType, bool) {
	for _, identification := range fallbackIdentifications {
		if identification.format == formatType && identification.dataType != "" {
			return identification.dataType, true
		}
	}
	return "", false
}

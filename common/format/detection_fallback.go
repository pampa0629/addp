package format

import "strings"

type fallbackIdentification struct {
	format      FormatType
	extensions  []string
	mimeTypes   []string
	primaryMIME string
}

var fallbackIdentifications = []fallbackIdentification{
	{FormatShapefile, []string{".shp"}, []string{"application/x-shapefile"}, "application/x-shapefile"},
	{FormatGeoPackage, []string{".gpkg"}, []string{"application/geopackage+sqlite3"}, "application/geopackage+sqlite3"},
	{FormatKML, []string{".kml"}, []string{"application/vnd.google-earth.kml+xml"}, "application/vnd.google-earth.kml+xml"},
	{FormatKMZ, []string{".kmz"}, []string{"application/vnd.google-earth.kmz"}, "application/vnd.google-earth.kmz"},
	{FormatCSV, []string{".csv"}, []string{"text/csv"}, "text/csv"},
	{FormatExcel, []string{".xlsx", ".xls"}, []string{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.ms-excel"}, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	{FormatTSV, []string{".tsv"}, []string{"text/tab-separated-values"}, "text/tab-separated-values"},
	{FormatPDF, []string{".pdf"}, []string{"application/pdf"}, "application/pdf"},
	{FormatDOCX, []string{".docx"}, []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"}, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	{FormatPPTX, []string{".pptx"}, []string{"application/vnd.openxmlformats-officedocument.presentationml.presentation"}, "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	{FormatWPS, []string{".wps"}, []string{"application/vnd.ms-works", "application/wps-office.doc", "application/x-wps", "application/kswps"}, "application/vnd.ms-works"},
	{FormatText, []string{".txt"}, []string{"text/plain"}, "text/plain"},
	{FormatMarkdown, []string{".md", ".markdown"}, []string{"text/markdown", "text/x-markdown"}, "text/markdown"},
	{FormatJPEG, []string{".jpg", ".jpeg"}, []string{"image/jpeg"}, "image/jpeg"},
	{FormatPNG, []string{".png"}, []string{"image/png"}, "image/png"},
	{FormatGIF, []string{".gif"}, []string{"image/gif"}, "image/gif"},
	{FormatTIFF, []string{".tif", ".tiff"}, []string{"image/tiff"}, "image/tiff"},
	{FormatWebP, []string{".webp"}, []string{"image/webp"}, "image/webp"},
	{FormatBMP, []string{".bmp"}, []string{"image/bmp", "image/x-ms-bmp"}, "image/bmp"},
	{FormatSVG, []string{".svg", ".svgz"}, []string{"image/svg+xml"}, "image/svg+xml"},
	{FormatAVIF, []string{".avif"}, []string{"image/avif"}, "image/avif"},
	{FormatHEIC, []string{".heic", ".heif"}, []string{"image/heic", "image/heif", "image/heic-sequence", "image/heif-sequence"}, "image/heic"},
	{FormatSQLite, []string{".sqlite", ".db", ".sqlite3"}, []string{"application/x-sqlite3", "application/vnd.sqlite3", "application/sqlite"}, "application/x-sqlite3"},
	{FormatJSON, []string{".json", ".geojson"}, []string{"application/json", "application/geo+json", "application/vnd.geo+json"}, "application/json"},
	{FormatXML, []string{".xml"}, []string{"application/xml", "text/xml"}, "application/xml"},
	{FormatParquet, []string{".parquet"}, []string{"application/x-parquet", "application/parquet", "application/vnd.apache.parquet"}, "application/x-parquet"},
	{FormatORC, []string{".orc"}, []string{"application/orc", "application/vnd.apache.orc"}, "application/orc"},
	{FormatAvro, []string{".avro"}, []string{"application/avro", "application/x-avro-binary"}, "application/avro"},
	{FormatZIP, []string{".zip"}, []string{"application/zip", "application/x-zip-compressed"}, "application/zip"},
	{FormatMP4, []string{".mp4", ".m4v"}, []string{"video/mp4", "application/mp4"}, "video/mp4"},
	{FormatAVI, []string{".avi"}, []string{"video/x-msvideo", "video/avi"}, "video/x-msvideo"},
	{FormatMOV, []string{".mov", ".qt"}, []string{"video/quicktime"}, "video/quicktime"},
	{FormatMKV, []string{".mkv"}, []string{"video/x-matroska", "video/matroska"}, "video/x-matroska"},
	{FormatWebM, []string{".webm"}, []string{"video/webm"}, "video/webm"},
	{FormatMP3, []string{".mp3"}, []string{"audio/mpeg", "audio/mp3"}, "audio/mpeg"},
	{FormatWAV, []string{".wav"}, []string{"audio/wav", "audio/wave", "audio/x-wav"}, "audio/wav"},
	{FormatFLAC, []string{".flac"}, []string{"audio/flac"}, "audio/flac"},
	{FormatAAC, []string{".aac", ".m4a"}, []string{"audio/aac", "audio/mp4", "audio/x-m4a"}, "audio/aac"},
	{FormatOGG, []string{".ogg", ".oga", ".opus"}, []string{"audio/ogg", "audio/opus"}, "audio/ogg"},
	{FormatVideo, []string{".flv", ".wmv"}, []string{"video/*"}, "video/*"},
	{FormatAudio, nil, []string{"audio/*"}, "audio/*"},
	{FormatImage, nil, []string{"image/*"}, "image/*"},
}

// fallbackFormatByExtension keeps detection usable before builtin descriptors
// are imported. Format descriptors remain the primary fact source.
func fallbackFormatByExtension(ext string) FormatType {
	for _, identification := range fallbackIdentifications {
		for _, candidate := range identification.extensions {
			if candidate == ext {
				return identification.format
			}
		}
	}
	return FormatUnknown
}

// fallbackFormatByMIME keeps detection usable before builtin descriptors are
// imported. Format descriptors remain the primary fact source.
func fallbackFormatByMIME(mimeType string) FormatType {
	for _, identification := range fallbackIdentifications {
		for _, candidate := range identification.mimeTypes {
			if candidate == mimeType || (strings.HasSuffix(candidate, "/*") && strings.HasPrefix(mimeType, strings.TrimSuffix(candidate, "*"))) {
				return identification.format
			}
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

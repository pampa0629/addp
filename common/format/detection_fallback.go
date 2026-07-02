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
	{FormatShapefile, ".shp", "application/x-shapefile", datatype.Table},
	{FormatGeoPackage, ".gpkg", "application/geopackage+sqlite3", datatype.Container},
	{FormatKML, ".kml", "application/vnd.google-earth.kml+xml", datatype.Document},
	{FormatKMZ, ".kmz", "application/vnd.google-earth.kmz", datatype.Container},
	{FormatCSV, ".csv", "text/csv", datatype.Table},
	{FormatExcel, ".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", datatype.Container},
	{FormatTSV, ".tsv", "text/tab-separated-values", datatype.Table},
	{FormatPDF, ".pdf", "application/pdf", datatype.Document},
	{FormatDOCX, ".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", datatype.Document},
	{FormatPPTX, ".pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", datatype.Document},
	{FormatWPS, ".wps", "application/vnd.ms-works", datatype.Document},
	{FormatText, ".txt", "text/plain", datatype.Document},
	{FormatMarkdown, ".md", "text/markdown", datatype.Document},
	{FormatJPEG, ".jpg", "image/jpeg", datatype.Media},
	{FormatPNG, ".png", "image/png", datatype.Media},
	{FormatGIF, ".gif", "image/gif", datatype.Media},
	{FormatTIFF, ".tif", "image/tiff", datatype.Media},
	{FormatWebP, ".webp", "image/webp", datatype.Media},
	{FormatBMP, ".bmp", "image/bmp", datatype.Media},
	{FormatSVG, ".svg", "image/svg+xml", datatype.Media},
	{FormatAVIF, ".avif", "image/avif", datatype.Media},
	{FormatHEIC, ".heic", "image/heic", datatype.Media},
	{FormatSQLite, ".sqlite", "application/x-sqlite3", datatype.Container},
	{FormatJSON, ".json", "application/json", datatype.Table},
	{FormatGeoJSON, ".geojson", "application/geo+json", datatype.Table},
	{FormatXML, ".xml", "application/xml", datatype.Document},
	{FormatParquet, ".parquet", "application/parquet", datatype.Table},
	{FormatORC, ".orc", "application/orc", datatype.Table},
	{FormatAvro, ".avro", "application/avro", datatype.Table},
	{FormatZIP, ".zip", "application/zip", datatype.Container},
	{FormatMP4, ".mp4", "video/mp4", datatype.Media},
	{FormatAVI, ".avi", "video/x-msvideo", datatype.Media},
	{FormatMOV, ".mov", "video/quicktime", datatype.Media},
	{FormatMKV, ".mkv", "video/x-matroska", datatype.Media},
	{FormatWebM, ".webm", "video/webm", datatype.Media},
	{FormatMP3, ".mp3", "audio/mpeg", datatype.Media},
	{FormatWAV, ".wav", "audio/wav", datatype.Media},
	{FormatFLAC, ".flac", "audio/flac", datatype.Media},
	{FormatAAC, ".aac", "audio/aac", datatype.Media},
	{FormatOGG, ".ogg", "audio/ogg", datatype.Media},
	{FormatVideo, "", "video/*", datatype.Media},
	{FormatAudio, "", "audio/*", datatype.Media},
	{FormatImage, "", "image/*", datatype.Media},
	{FormatGLB, ".glb", "model/gltf-binary", datatype.Model3D},
	{FormatGLTF, ".gltf", "model/gltf+json", datatype.Model3D},
	{FormatOBJ, ".obj", "model/obj", datatype.Model3D},
	{FormatSTL, ".stl", "model/stl", datatype.Model3D},
	{FormatFBX, ".fbx", "application/vnd.autodesk.fbx", datatype.Model3D},
	{FormatIFC, ".ifc", "application/x-step", datatype.Model3D},
	{FormatPLY, ".ply", "model/ply", datatype.Model3D},
	{FormatSplat, ".splat", "application/vnd.gaussian-splat", datatype.GaussianSplat},
	{FormatKSplat, ".ksplat", "application/vnd.gaussian-ksplat", datatype.GaussianSplat},
	{FormatLAS, ".las", "application/vnd.las", datatype.PointCloud},
	{Format3DTiles, "", "application/vnd.ogc.3dtiles+json", datatype.Model3D},
	{FormatOSGB, ".osgb", "application/octet-stream", datatype.Model3D},
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

package format

import formatregistry "github.com/addp/common/format/registry"

// FormatType 是 ADDP 内稳定的文件/逻辑格式标识。
type FormatType = formatregistry.Format

const (
	FormatShapefile  = formatregistry.FormatShapefile
	FormatGeoPackage = formatregistry.FormatGeoPackage
	FormatKML        = formatregistry.FormatKML
	FormatKMZ        = formatregistry.FormatKMZ

	FormatCSV   = formatregistry.FormatCSV
	FormatExcel = formatregistry.FormatExcel
	FormatTSV   = formatregistry.FormatTSV

	FormatPDF      = formatregistry.FormatPDF
	FormatDOCX     = formatregistry.FormatDOCX
	FormatPPTX     = formatregistry.FormatPPTX
	FormatWPS      = formatregistry.FormatWPS
	FormatText     = formatregistry.FormatText
	FormatMarkdown = formatregistry.FormatMarkdown

	FormatImage = formatregistry.FormatImage
	FormatJPEG  = formatregistry.FormatJPEG
	FormatPNG   = formatregistry.FormatPNG
	FormatGIF   = formatregistry.FormatGIF
	FormatTIFF  = formatregistry.FormatTIFF
	FormatWebP  = formatregistry.FormatWebP
	FormatBMP   = formatregistry.FormatBMP
	FormatSVG   = formatregistry.FormatSVG
	FormatAVIF  = formatregistry.FormatAVIF
	FormatHEIC  = formatregistry.FormatHEIC

	FormatSQLite   = formatregistry.FormatSQLite
	FormatPostgres = formatregistry.FormatPostgres
	FormatMySQL    = formatregistry.FormatMySQL

	FormatJSON    = formatregistry.FormatJSON
	FormatXML     = formatregistry.FormatXML
	FormatParquet = formatregistry.FormatParquet
	FormatORC     = formatregistry.FormatORC
	FormatAvro    = formatregistry.FormatAvro
	FormatZIP     = formatregistry.FormatZIP

	FormatVideo = formatregistry.FormatVideo
	FormatAudio = formatregistry.FormatAudio
	FormatMP4   = formatregistry.FormatMP4
	FormatMOV   = formatregistry.FormatMOV
	FormatMKV   = formatregistry.FormatMKV
	FormatAVI   = formatregistry.FormatAVI
	FormatWebM  = formatregistry.FormatWebM
	FormatMP3   = formatregistry.FormatMP3
	FormatWAV   = formatregistry.FormatWAV
	FormatFLAC  = formatregistry.FormatFLAC
	FormatAAC   = formatregistry.FormatAAC
	FormatOGG   = formatregistry.FormatOGG

	FormatUnknown = formatregistry.FormatUnknown
)

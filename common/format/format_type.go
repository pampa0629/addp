package format

// FormatType 是 ADDP 内稳定的文件/逻辑格式标识。
type FormatType string

const (
	FormatShapefile  FormatType = "shapefile"
	FormatGeoPackage FormatType = "geopackage"
	FormatKML        FormatType = "kml"
	FormatKMZ        FormatType = "kmz"

	FormatCSV   FormatType = "csv"
	FormatExcel FormatType = "excel"
	FormatTSV   FormatType = "tsv"

	FormatPDF      FormatType = "pdf"
	FormatDOCX     FormatType = "docx"
	FormatPPTX     FormatType = "pptx"
	FormatWPS      FormatType = "wps"
	FormatText     FormatType = "text"
	FormatMarkdown FormatType = "markdown"

	FormatImage FormatType = "image"
	FormatJPEG  FormatType = "jpeg"
	FormatPNG   FormatType = "png"
	FormatGIF   FormatType = "gif"
	FormatTIFF  FormatType = "tiff"
	FormatWebP  FormatType = "webp"
	FormatBMP   FormatType = "bmp"
	FormatSVG   FormatType = "svg"
	FormatAVIF  FormatType = "avif"
	FormatHEIC  FormatType = "heic"

	FormatSQLite   FormatType = "sqlite"
	FormatPostgres FormatType = "postgres"
	FormatMySQL    FormatType = "mysql"

	FormatJSON    FormatType = "json"
	FormatXML     FormatType = "xml"
	FormatParquet FormatType = "parquet"
	FormatORC     FormatType = "orc"
	FormatAvro    FormatType = "avro"
	FormatZIP     FormatType = "zip"

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

	FormatUnknown FormatType = "unknown"
)

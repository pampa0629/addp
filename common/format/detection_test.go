package format

import (
	"testing"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		peek     []byte
		want     FormatType
	}{
		{
			name:     "Shapefile by extension",
			filename: "data.shp",
			peek:     []byte{0x00, 0x00, 0x27, 0x0a}, // Shapefile magic
			want:     FormatShapefile,
		},
		{
			name:     "Shapefile index component is not full shapefile format",
			filename: "data.shx",
			peek:     nil,
			want:     FormatUnknown,
		},
		{
			name:     "Shapefile attributes component is not full shapefile format",
			filename: "data.dbf",
			peek:     nil,
			want:     FormatUnknown,
		},
		{
			name:     "Shapefile projection component is plain text fallback only at preview stage",
			filename: "data.prj",
			peek:     nil,
			want:     FormatUnknown,
		},
		{
			name:     "Shapefile encoding component is not full shapefile format",
			filename: "data.cpg",
			peek:     nil,
			want:     FormatUnknown,
		},
		{
			name:     "GeoJSON extension is JSON format",
			filename: "data.geojson",
			peek:     nil,
			want:     FormatJSON,
		},
		{
			name:     "CSV by extension",
			filename: "data.csv",
			peek:     nil,
			want:     FormatCSV,
		},
		{
			name:     "ORC by extension",
			filename: "data.orc",
			peek:     nil,
			want:     FormatORC,
		},
		{
			name:     "Markdown by extension",
			filename: "README.md",
			peek:     nil,
			want:     FormatMarkdown,
		},
		{
			name:     "PDF with validation",
			filename: "document.pdf",
			peek:     []byte("%PDF-1.4"),
			want:     FormatPDF,
		},
		{
			name:     "PDF with wrong magic bytes",
			filename: "document.pdf",
			peek:     []byte("not a pdf"),
			want:     FormatUnknown,
		},
		{
			name:     "JPEG by extension",
			filename: "image.jpg",
			peek:     []byte{0xFF, 0xD8, 0xFF},
			want:     FormatJPEG,
		},
		{
			name:     "PNG by magic bytes",
			filename: "unknown",
			peek:     []byte{0x89, 0x50, 0x4E, 0x47},
			want:     FormatPNG,
		},
		{
			name:     "WebP by extension",
			filename: "image.webp",
			peek:     nil,
			want:     FormatWebP,
		},
		{
			name:     "WebP by magic bytes",
			filename: "unknown",
			peek:     []byte("RIFF\x24\x00\x00\x00WEBPVP8 "),
			want:     FormatWebP,
		},
		{
			name:     "SVG by magic bytes",
			filename: "unknown",
			peek:     []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`),
			want:     FormatSVG,
		},
		{
			name:     "MP4 by extension",
			filename: "clip.mp4",
			peek:     nil,
			want:     FormatMP4,
		},
		{
			name:     "MOV by MIME family magic brand",
			filename: "unknown",
			peek:     []byte("\x00\x00\x00\x18ftypqt  \x00\x00\x00\x00"),
			want:     FormatMOV,
		},
		{
			name:     "MP3 by extension",
			filename: "audio.mp3",
			peek:     nil,
			want:     FormatMP3,
		},
		{
			name:     "WAV by magic bytes",
			filename: "unknown",
			peek:     []byte("RIFF\x24\x00\x00\x00WAVEfmt "),
			want:     FormatWAV,
		},
		{
			name:     "SQLite by extension",
			filename: "data.sqlite",
			peek:     []byte("SQLite format 3"),
			want:     FormatSQLite,
		},
		{
			name:     "Unknown format",
			filename: "data.unknown",
			peek:     nil,
			want:     FormatUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFormat(tt.filename, tt.peek)
			if got != tt.want {
				t.Errorf("DetectFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMIMEToFormat(t *testing.T) {
	tests := []struct {
		mimeType string
		want     FormatType
	}{
		{"application/geo+json", FormatJSON},
		{"application/vnd.geo+json", FormatJSON},
		{"text/csv", FormatCSV},
		{"application/pdf", FormatPDF},
		{"application/vnd.ms-works", FormatWPS},
		{"application/wps-office.doc", FormatWPS},
		{"text/markdown", FormatMarkdown},
		{"text/x-markdown", FormatMarkdown},
		{"image/jpeg", FormatJPEG},
		{"image/png", FormatPNG},
		{"image/webp", FormatWebP},
		{"image/svg+xml", FormatSVG},
		{"application/json", FormatJSON},
		{"application/x-sqlite3", FormatSQLite},
		{"application/vnd.apache.orc", FormatORC},
		{"video/mp4", FormatMP4},
		{"video/quicktime", FormatMOV},
		{"video/x-matroska", FormatMKV},
		{"audio/mpeg", FormatMP3},
		{"audio/wav", FormatWAV},
		{"unknown/type", FormatUnknown},
		{"text/csv; charset=utf-8", FormatCSV}, // 带参数
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := MIMEToFormat(tt.mimeType)
			if got != tt.want {
				t.Errorf("MIMEToFormat(%q) = %v, want %v", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestFormatToMIME(t *testing.T) {
	tests := []struct {
		format FormatType
		want   string
	}{
		{FormatCSV, "text/csv"},
		{FormatPDF, "application/pdf"},
		{FormatWPS, "application/vnd.ms-works"},
		{FormatMarkdown, "text/markdown"},
		{FormatJPEG, "image/jpeg"},
		{FormatWebP, "image/webp"},
		{FormatSVG, "image/svg+xml"},
		{FormatMP4, "video/mp4"},
		{FormatMP3, "audio/mpeg"},
		{FormatSQLite, "application/x-sqlite3"},
		{FormatORC, "application/orc"},
		{FormatShapefile, "application/x-shapefile"},
		{FormatUnknown, "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			got := FormatToMIME(tt.format)
			if got != tt.want {
				t.Errorf("FormatToMIME(%v) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestDetectionUsesFormatDescriptors(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		mimeType string
		want     FormatType
	}{
		{
			name:     "text extension",
			filename: "notes.txt",
			want:     FormatText,
		},
		{
			name:     "markdown extension",
			filename: "README.markdown",
			want:     FormatMarkdown,
		},
		{
			name:     "markdown mime",
			mimeType: "text/x-markdown",
			want:     FormatMarkdown,
		},
		{
			name:     "parquet mime",
			mimeType: "application/vnd.apache.parquet",
			want:     FormatParquet,
		},
		{
			name:     "wps mime from descriptor",
			mimeType: "application/kswps",
			want:     FormatWPS,
		},
		{
			name:     "excel macro extension from descriptor",
			filename: "book.xlsm",
			want:     FormatExcel,
		},
		{
			name:     "sqlite mime from descriptor",
			mimeType: "application/vnd.sqlite3",
			want:     FormatSQLite,
		},
		{
			name:     "webp extension from descriptor",
			filename: "image.webp",
			want:     FormatWebP,
		},
		{
			name:     "svg mime from descriptor",
			mimeType: "image/svg+xml",
			want:     FormatSVG,
		},
		{
			name:     "mp4 extension from descriptor",
			filename: "video.mp4",
			want:     FormatMP4,
		},
		{
			name:     "wav mime from descriptor",
			mimeType: "audio/wave",
			want:     FormatWAV,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.filename != "" {
				got := DetectFormat(tt.filename, nil)
				if got != tt.want {
					t.Fatalf("DetectFormat(%q) = %s, want %s", tt.filename, got, tt.want)
				}
			}
			if tt.mimeType != "" {
				got := MIMEToFormat(tt.mimeType)
				if got != tt.want {
					t.Fatalf("MIMEToFormat(%q) = %s, want %s", tt.mimeType, got, tt.want)
				}
			}
		})
	}
}

func TestIsGeospatialFormat(t *testing.T) {
	tests := []struct {
		format FormatType
		want   bool
	}{
		{FormatShapefile, true},
		{FormatGeoPackage, true},
		{FormatKML, true},
		{FormatCSV, false},
		{FormatPDF, false},
		{FormatImage, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			got := IsGeospatialFormat(tt.format)
			if got != tt.want {
				t.Errorf("IsGeospatialFormat(%v) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestIsDocumentFormat(t *testing.T) {
	tests := []struct {
		format FormatType
		want   bool
	}{
		{FormatPDF, true},
		{FormatDOCX, true},
		{FormatPPTX, true},
		{FormatText, true},
		{FormatMarkdown, true},
		{FormatCSV, false},
		{FormatImage, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			got := IsDocumentFormat(tt.format)
			if got != tt.want {
				t.Errorf("IsDocumentFormat(%v) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestIsImageFormat(t *testing.T) {
	tests := []struct {
		format FormatType
		want   bool
	}{
		{FormatJPEG, true},
		{FormatPNG, true},
		{FormatGIF, true},
		{FormatWebP, true},
		{FormatSVG, true},
		{FormatImage, true},
		{FormatMP4, false},
		{FormatMP3, false},
		{FormatPDF, false},
		{FormatCSV, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			got := IsImageFormat(tt.format)
			if got != tt.want {
				t.Errorf("IsImageFormat(%v) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestIsTableFormat(t *testing.T) {
	tests := []struct {
		format FormatType
		want   bool
	}{
		{FormatCSV, true},
		{FormatExcel, true},
		{FormatTSV, true},
		{FormatPDF, false},
		{FormatJSON, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			got := IsTableFormat(tt.format)
			if got != tt.want {
				t.Errorf("IsTableFormat(%v) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestGuessContentType(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		peek     []byte
		want     string
	}{
		{
			name:     "CSV file",
			filename: "data.csv",
			peek:     nil,
			want:     "text/csv; charset=utf-8",
		},
		{
			name:     "Shapefile (custom MIME)",
			filename: "data.shp",
			peek:     []byte{0x00, 0x00, 0x27, 0x0a},
			want:     "application/x-shapefile",
		},
		{
			name:     "PDF file",
			filename: "document.pdf",
			peek:     []byte("%PDF-1.4"),
			want:     "application/pdf",
		},
		{
			name:     "WPS file",
			filename: "document.wps",
			peek:     nil,
			want:     "application/vnd.ms-works",
		},
		{
			name:     "Markdown file",
			filename: "README.md",
			peek:     nil,
			want:     "text/markdown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GuessContentType(tt.filename, tt.peek)
			if got != tt.want {
				t.Errorf("GuessContentType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func BenchmarkDetectFormat(b *testing.B) {
	filename := "data.geojson"
	peek := []byte(`{"type":"FeatureCollection"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DetectFormat(filename, peek)
	}
}

func BenchmarkMIMEToFormat(b *testing.B) {
	mimeType := "application/geo+json"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MIMEToFormat(mimeType)
	}
}

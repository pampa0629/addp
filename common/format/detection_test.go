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
		{"text/markdown", FormatMarkdown},
		{"text/x-markdown", FormatMarkdown},
		{"image/jpeg", FormatJPEG},
		{"image/png", FormatPNG},
		{"application/json", FormatJSON},
		{"application/x-sqlite3", FormatSQLite},
		{"video/mp4", FormatVideo},
		{"audio/mpeg", FormatAudio},
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
		{FormatMarkdown, "text/markdown"},
		{FormatJPEG, "image/jpeg"},
		{FormatSQLite, "application/x-sqlite3"},
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
		{FormatImage, true},
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

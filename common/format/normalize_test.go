package format_test

import (
	"testing"

	. "github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestNormalizeFormatReturnsCanonicalFormatOnly(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  FormatType
	}{
		{name: "canonical format", value: "CSV", want: FormatCSV},
		{name: "extension", value: ".xlsx", want: FormatExcel},
		{name: "filename", value: "table.orc", want: FormatORC},
		{name: "mime type", value: "text/csv; charset=utf-8", want: FormatCSV},
		{name: "yaml extension without content stays unknown", value: ".yml", want: FormatUnknown},
		{name: "unknown bare value stays unknown", value: "custom-binary", want: FormatUnknown},
		{name: "unknown mime stays unknown", value: "application/x-custom-binary", want: FormatUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeFormat(tt.value); got != tt.want {
				t.Fatalf("NormalizeFormat(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

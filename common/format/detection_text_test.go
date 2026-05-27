package format_test

import (
	"testing"

	. "github.com/addp/common/format"
)

func TestLooksLikeTextContent(t *testing.T) {
	tests := []struct {
		name string
		peek []byte
		want bool
	}{
		{name: "empty", peek: nil, want: false},
		{name: "utf8 text", peek: []byte("hello\n世界"), want: true},
		{name: "utf8 bom text", peek: []byte("\ufeffhello"), want: true},
		{name: "nul binary", peek: []byte{'h', 0, 'i'}, want: false},
		{name: "invalid utf8", peek: []byte{0xff, 0xfe, 0xfd}, want: false},
		{name: "many controls", peek: []byte{0x01, 0x02, 0x03, 'a'}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksLikeTextContent(tt.peek); got != tt.want {
				t.Fatalf("LooksLikeTextContent() = %v, want %v", got, tt.want)
			}
		})
	}
}

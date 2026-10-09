package engineaccess

import (
	"strings"
	"testing"
)

func TestSourceGrantFilterValidation(t *testing.T) {
	for _, filter := range []SourceGrantFilter{{}, {TableSearch: "表_%", AccountID: 9007199254740993}, {TableSearch: strings.Repeat("表", 200)}} {
		if err := filter.Validate(); err != nil {
			t.Fatalf("valid filter rejected: %+v %v", filter, err)
		}
	}
	for _, filter := range []SourceGrantFilter{{AccountID: -1}, {TableSearch: " a"}, {TableSearch: "a\n"}, {TableSearch: "a\x00b"}, {TableSearch: "\xff"}, {TableSearch: strings.Repeat("表", 201)}} {
		if err := filter.Validate(); err == nil {
			t.Fatalf("invalid filter accepted: %+v", filter)
		}
	}
}

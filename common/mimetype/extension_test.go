package mimetype_test

import (
	"testing"

	"github.com/addp/common/mimetype"
)

func TestExtensionInferenceDoesNotClaimCharset(t *testing.T) {
	for _, tc := range []struct{ extension, want string }{
		{".csv", "text/csv"}, {".CSV", "text/csv"},
		{".txt", "text/plain"}, {".html", "text/html"},
		{".png", "image/png"}, {".json", "application/json"},
		{".addp-unknown-extension", ""}, {"", ""},
	} {
		t.Run(tc.extension, func(t *testing.T) {
			if got := mimetype.ByExtension(tc.extension); got != tc.want {
				t.Fatalf("ByExtension(%q) = %q, want %q", tc.extension, got, tc.want)
			}
		})
	}
}

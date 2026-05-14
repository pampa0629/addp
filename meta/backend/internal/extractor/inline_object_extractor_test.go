package extractor

import (
	"testing"

	_ "github.com/addp/common/format/builtin"
)

func TestInlineObjectMetadataExtractorShouldExtract(t *testing.T) {
	t.Parallel()

	extractor := NewInlineObjectMetadataExtractor(nil)
	if !extractor.ShouldExtract("image.png", "image/png", 1024) {
		t.Fatalf("image/png should be extracted")
	}
	if !extractor.ShouldExtract("image.png", "", 1024) {
		t.Fatalf("image extension should be extracted")
	}
	if extractor.ShouldExtract("document.pdf", "application/pdf", 1024) {
		t.Fatalf("pdf should not be extracted inline")
	}
	if extractor.ShouldExtract("image.png", "image/png", 101*1024*1024) {
		t.Fatalf("large image should not be extracted inline")
	}
}

package extractor

import "testing"

func TestInlineObjectMetadataExtractorShouldExtract(t *testing.T) {
	t.Parallel()

	extractor := NewInlineObjectMetadataExtractor(nil, nil)
	if !extractor.ShouldExtract("image/png", 1024) {
		t.Fatalf("image/png should be extracted")
	}
	if extractor.ShouldExtract("application/pdf", 1024) {
		t.Fatalf("pdf should not be extracted inline")
	}
	if extractor.ShouldExtract("image/png", 101*1024*1024) {
		t.Fatalf("large image should not be extracted inline")
	}
}

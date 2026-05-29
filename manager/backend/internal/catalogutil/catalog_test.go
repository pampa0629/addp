package catalogutil

import "testing"

func TestAttributeHelpersReadStandardTypeInfoSections(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"duration":   float64(99),
		"codec":      "legacy-codec",
		"page_count": float64(3),
		"type_info": map[string]interface{}{
			"media": map[string]interface{}{
				"width":       float64(640),
				"height":      float64(480),
				"duration_ms": float64(1200),
				"kind":        "video",
				"encoding":    "h264",
				"color_space": "yuv420p",
				"size_bytes":  float64(4096),
			},
			"document": map[string]interface{}{
				"page_count": float64(12),
				"word_count": float64(345),
				"encoding":   "utf-8",
			},
		},
	}

	if got := Int64Attribute(attrs, "duration_ms"); got != 1200 {
		t.Fatalf("duration_ms = %d, want 1200", got)
	}
	if got := StringAttribute(attrs, "kind"); got != "video" {
		t.Fatalf("kind = %q, want video", got)
	}
	if got := Int64Attribute(attrs, "size_bytes"); got != 4096 {
		t.Fatalf("size_bytes = %d, want 4096", got)
	}
	if got := Int64Attribute(attrs, "duration"); got != 0 {
		t.Fatalf("legacy duration = %d, want 0", got)
	}
	if got := StringAttribute(attrs, "codec"); got != "" {
		t.Fatalf("legacy codec = %q, want empty", got)
	}
	if got := StringAttribute(attrs, "encoding"); got != "utf-8" {
		t.Fatalf("document encoding = %q, want utf-8", got)
	}
	if got := Int64Attribute(attrs, "page_count"); got != 12 {
		t.Fatalf("page_count = %d, want 12", got)
	}
	if got := Int64Attribute(map[string]interface{}{"page_count": float64(3)}, "page_count"); got != 0 {
		t.Fatalf("flat page_count = %d, want 0", got)
	}
	if got := StringAttribute(map[string]interface{}{"kind": "legacy-media-kind"}, "kind"); got != "" {
		t.Fatalf("flat kind = %q, want empty", got)
	}
	if got := StringAttribute(map[string]interface{}{
		"type_info": map[string]interface{}{"media": map[string]interface{}{"encoding": "jpeg"}},
	}, "encoding"); got != "jpeg" {
		t.Fatalf("media encoding = %q, want jpeg", got)
	}
}

func TestAttributeHelpersReadStandardExtractionSection(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"plain_text_preview": "legacy preview",
		"capabilities": map[string]interface{}{
			"extraction": map[string]interface{}{
				"extractor_available": true,
				"text_extracted":      true,
				"status":              "completed",
				"reason":              "ok",
				"extractor":           "common_format:docx",
				"plain_text_preview":  "standard preview",
				"text_truncated":      true,
				"index_ref":           "meilisearch:assets:fingerprint",
			},
		},
	}

	for _, key := range []string{"status", "reason", "extractor", "plain_text_preview", "index_ref"} {
		if got := StringAttribute(attrs, key); got == "" {
			t.Fatalf("%s should be read from capabilities.extraction", key)
		}
	}
	if got := StringAttribute(attrs, "plain_text_preview"); got != "standard preview" {
		t.Fatalf("plain_text_preview = %q, want standard preview", got)
	}
	if got := StringAttribute(map[string]interface{}{"plain_text_preview": "legacy preview"}, "plain_text_preview"); got != "" {
		t.Fatalf("flat plain_text_preview = %q, want empty", got)
	}
}

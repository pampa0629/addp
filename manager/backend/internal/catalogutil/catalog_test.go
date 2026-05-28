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
				"encoding":    "h264",
				"color_space": "yuv420p",
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
}

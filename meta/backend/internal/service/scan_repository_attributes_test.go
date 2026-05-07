package service

import "testing"

func TestIndexerAttributeReadersPreferStandardTypeInfo(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"title":      "legacy title",
		"page_count": 1,
		"type_info": map[string]interface{}{
			"document": map[string]interface{}{
				"title":      "standard title",
				"page_count": 12,
				"keywords":   []interface{}{"alpha", "beta"},
			},
		},
	}

	if got := stringFromAttributes(attrs, "document", "title"); got != "standard title" {
		t.Fatalf("title = %q, want standard title", got)
	}
	if got := intFromAttributes(attrs, "document", "page_count"); got != 12 {
		t.Fatalf("page_count = %d, want 12", got)
	}
	keywords := stringSliceFromAttributes(attrs, "document", "keywords")
	if len(keywords) != 2 || keywords[0] != "alpha" {
		t.Fatalf("keywords = %#v, want type_info keywords", keywords)
	}

	standardOnly := map[string]interface{}{
		"title":      "legacy title",
		"page_count": 1,
	}
	if got := stringFromAttributes(standardOnly, "document", "title"); got != "" {
		t.Fatalf("legacy title fallback = %q, want empty", got)
	}
	if got := intFromAttributes(standardOnly, "document", "page_count"); got != 0 {
		t.Fatalf("legacy page_count fallback = %d, want 0", got)
	}
}

func TestIndexerReadsPlainTextFromStandardExtractionPayload(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"plain_text": "legacy text",
		"capabilities": map[string]interface{}{
			"extraction": map[string]interface{}{
				"extracted_metadata": map[string]interface{}{
					"custom_attrs": map[string]interface{}{
						"plain_text": "standard text",
					},
				},
			},
		},
	}

	if got := extractedPlainTextFromAttributes(attrs); got != "standard text" {
		t.Fatalf("plain text = %q, want standard text", got)
	}
	if got := extractedPlainTextFromAttributes(map[string]interface{}{"plain_text": "legacy text"}); got != "" {
		t.Fatalf("legacy plain text fallback = %q, want empty", got)
	}
}

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
			},
		},
		"capabilities": map[string]interface{}{
			"extraction": map[string]interface{}{
				"keywords": []interface{}{"alpha", "beta"},
			},
		},
	}

	if got := stringFromStandardAttributes(attrs, "type_info.document", "title"); got != "standard title" {
		t.Fatalf("title = %q, want standard title", got)
	}
	if got := intFromStandardAttributes(attrs, "type_info.document", "page_count"); got != 12 {
		t.Fatalf("page_count = %d, want 12", got)
	}
	keywords := stringSliceFromStandardAttributes(attrs, "capabilities.extraction", "keywords")
	if len(keywords) != 2 || keywords[0] != "alpha" {
		t.Fatalf("keywords = %#v, want extraction keywords", keywords)
	}

	standardOnly := map[string]interface{}{
		"title":      "legacy title",
		"page_count": 1,
	}
	if got := stringFromStandardAttributes(standardOnly, "type_info.document", "title"); got != "" {
		t.Fatalf("legacy title fallback = %q, want empty", got)
	}
	if got := intFromStandardAttributes(standardOnly, "type_info.document", "page_count"); got != 0 {
		t.Fatalf("legacy page_count fallback = %d, want 0", got)
	}
}

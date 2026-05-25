package service

import "testing"

func TestSearchAttributeReadersPreferStandardSections(t *testing.T) {
	t.Parallel()

	meta := map[string]interface{}{
		"name":  "flat.pdf",
		"title": "flat title",
		"storage": map[string]interface{}{
			"name": "standard.pdf",
		},
		"type_info": map[string]interface{}{
			"document": map[string]interface{}{
				"title": "standard title",
			},
		},
		"item": map[string]interface{}{
			"document_type": "pdf",
		},
	}

	if got := getStringFromMeta(meta, "name"); got != "standard.pdf" {
		t.Fatalf("name = %q, want standard.pdf", got)
	}
	if got := getStringFromMeta(meta, "title"); got != "standard title" {
		t.Fatalf("title = %q, want standard title", got)
	}
	if got := getStringFromMeta(map[string]interface{}{"name": "flat.pdf"}, "name"); got != "" {
		t.Fatalf("flat name fallback = %q, want empty", got)
	}
	if got := getStringFromMeta(map[string]interface{}{"title": "flat title"}, "title"); got != "" {
		t.Fatalf("flat title fallback = %q, want empty", got)
	}
	if got := getStringFromMeta(meta, "document_type"); got != "pdf" {
		t.Fatalf("document_type = %q, want pdf", got)
	}

	var assigned string
	assignStringFromAttributes(meta, "type_info.document", "title", &assigned)
	if assigned != "standard title" {
		t.Fatalf("assigned title = %q, want standard title", assigned)
	}
	assigned = ""
	assignStringFromAttributes(map[string]interface{}{"title": "flat title"}, "type_info.document", "title", &assigned)
	if assigned != "" {
		t.Fatalf("flat assigned title fallback = %q, want empty", assigned)
	}
}

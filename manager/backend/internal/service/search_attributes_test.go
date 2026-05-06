package service

import "testing"

func TestSearchAttributeReadersPreferStandardSections(t *testing.T) {
	t.Parallel()

	meta := map[string]interface{}{
		"name":  "legacy.pdf",
		"title": "legacy title",
		"storage": map[string]interface{}{
			"name": "standard.pdf",
		},
		"type_info": map[string]interface{}{
			"document": map[string]interface{}{
				"title": "standard title",
			},
		},
	}

	if got := getStringFromMeta(meta, "name"); got != "standard.pdf" {
		t.Fatalf("name = %q, want standard.pdf", got)
	}
	if got := getStringFromMeta(meta, "title"); got != "standard title" {
		t.Fatalf("title = %q, want standard title", got)
	}

	var assigned string
	assignStringFromAttributes(meta, "type_info.document", "title", &assigned)
	if assigned != "standard title" {
		t.Fatalf("assigned title = %q, want standard title", assigned)
	}
}

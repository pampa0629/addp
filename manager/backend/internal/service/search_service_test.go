package service

import "testing"

func TestBuildSearchFilterRestrictsCurrentEngine(t *testing.T) {
	t.Parallel()

	tenantID := uint(7)
	engineID := uint(3)
	if got := buildSearchFilter(&tenantID, &engineID); got != "tenant_id = 7 AND engine_id = 3" {
		t.Fatalf("buildSearchFilter() = %q", got)
	}
}

func TestBuildSearchFilterAllowsTenantWideSearch(t *testing.T) {
	t.Parallel()

	tenantID := uint(7)
	if got := buildSearchFilter(&tenantID, nil); got != "tenant_id = 7" {
		t.Fatalf("buildSearchFilter() = %q", got)
	}
}

func TestMapMeilisearchHitKeepsOnlyIndexedLocator(t *testing.T) {
	t.Parallel()

	doc := mapMeilisearchHit(map[string]interface{}{
		"document_id":    "item-fingerprint",
		"content_hash":   "content-sha256",
		"engine_id":      float64(9),
		"engine_type":    "minio",
		"data_item_type": "object",
		"full_name":      "addp/reports/report.docx",
		"name":           "report.docx",
	})

	if doc.DocumentID != "item-fingerprint" {
		t.Fatalf("DocumentID = %q, want item fingerprint", doc.DocumentID)
	}
	if doc.FileName != "report.docx" {
		t.Fatalf("FileName = %q, want report.docx", doc.FileName)
	}
	if doc.Locator != "" {
		t.Fatalf("Locator = %q, want empty locator when index does not provide one", doc.Locator)
	}
}

func TestMapMeilisearchHitPrefersIndexedLocator(t *testing.T) {
	t.Parallel()

	doc := mapMeilisearchHit(map[string]interface{}{
		"document_id":    "item-fingerprint",
		"locator":        "addp://engine/9/path/addp/reports/report.docx?type=object&item_id=7",
		"engine_id":      float64(9),
		"data_item_type": "object",
		"full_name":      "wrong/path.txt",
		"name":           "report.docx",
	})

	if doc.Locator != "addp://engine/9/path/addp/reports/report.docx?type=object&item_id=7" {
		t.Fatalf("Locator = %q, want indexed locator", doc.Locator)
	}
}

func TestMapMeilisearchHitRejectsLocatorWithoutItemID(t *testing.T) {
	t.Parallel()

	doc := mapMeilisearchHit(map[string]interface{}{
		"document_id":    "item-fingerprint",
		"locator":        "addp://engine/9/path/addp/reports/report.docx?type=object",
		"engine_id":      float64(9),
		"data_item_type": "object",
		"full_name":      "addp/reports/report.docx",
		"name":           "report.docx",
	})

	if doc.Locator != "" {
		t.Fatalf("Locator = %q, want empty locator without item_id", doc.Locator)
	}
}

func TestVectorDocumentToSearchDocumentKeepsMetadataLocator(t *testing.T) {
	t.Parallel()

	doc := vectorDocumentToSearchDocument(VectorDocument{
		DocumentID: "vector-doc",
		EngineID:   9,
		Metadata: map[string]interface{}{
			"locator": "addp://engine/9/path/addp/reports/report.docx?type=object&item_id=7",
			"storage": map[string]interface{}{
				"bucket": "addp",
				"path":   "reports/",
				"name":   "report.docx",
			},
		},
	})

	if doc.Locator != "addp://engine/9/path/addp/reports/report.docx?type=object&item_id=7" {
		t.Fatalf("Locator = %q, want indexed locator", doc.Locator)
	}
}

func TestVectorDocumentToSearchDocumentRejectsMetadataLocatorWithoutItemID(t *testing.T) {
	t.Parallel()

	doc := vectorDocumentToSearchDocument(VectorDocument{
		DocumentID: "vector-doc",
		EngineID:   9,
		Metadata: map[string]interface{}{
			"resource": map[string]interface{}{
				"locator": "addp://engine/9/path/addp/reports/report.docx?type=object",
			},
			"storage": map[string]interface{}{
				"bucket": "addp",
				"path":   "reports/",
				"name":   "report.docx",
			},
		},
	})

	if doc.Locator != "" {
		t.Fatalf("Locator = %q, want empty locator without item_id", doc.Locator)
	}
}

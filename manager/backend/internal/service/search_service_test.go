package service

import (
	"reflect"
	"testing"
)

func TestFuseSearchDocumentsRewardsHybridMatchesAndDeduplicates(t *testing.T) {
	t.Parallel()

	result := fuseSearchDocuments(
		[]SearchDocument{
			{DocumentID: "keyword-first", FileName: "first.docx"},
			{DocumentID: "hybrid", FileName: "hybrid.docx", Locator: "keyword-locator"},
		},
		2,
		[]VectorDocument{
			{DocumentID: "hybrid", Distance: 0.1, Locator: "vector-locator"},
			{DocumentID: "vector-only", Distance: 0.2, FileName: "semantic.jpg"},
		},
		1,
		10,
	)

	if result.Total != 3 {
		t.Fatalf("Total = %d, want 3", result.Total)
	}
	if got := searchDocumentIDs(result.Hits); !reflect.DeepEqual(got, []string{"hybrid", "keyword-first", "vector-only"}) {
		t.Fatalf("document order = %v", got)
	}
	if got := result.Hits[0].MatchMethods; !reflect.DeepEqual(got, []string{"keyword", "vector"}) {
		t.Fatalf("hybrid match methods = %v", got)
	}
	if result.Hits[0].VectorDistance != 0.1 {
		t.Fatalf("hybrid vector distance = %v", result.Hits[0].VectorDistance)
	}
	if result.Hits[0].Locator != "keyword-locator" {
		t.Fatalf("hybrid locator = %q, want keyword locator", result.Hits[0].Locator)
	}
	if result.Hits[0].Score <= result.Hits[1].Score || result.Hits[1].Score <= result.Hits[2].Score {
		t.Fatalf("scores are not strictly descending: %v", []float64{result.Hits[0].Score, result.Hits[1].Score, result.Hits[2].Score})
	}
}

func TestFuseSearchDocumentsInterleavesPureVectorHitsBeforePagination(t *testing.T) {
	t.Parallel()

	keywords := []SearchDocument{
		{DocumentID: "keyword-1"},
		{DocumentID: "keyword-2"},
		{DocumentID: "keyword-3"},
	}
	vectors := []VectorDocument{{DocumentID: "vector-1", Distance: 0.15}}

	firstPage := fuseSearchDocuments(keywords, 3, vectors, 1, 2)
	if got := searchDocumentIDs(firstPage.Hits); !reflect.DeepEqual(got, []string{"keyword-1", "vector-1"}) {
		t.Fatalf("first page order = %v", got)
	}
	secondPage := fuseSearchDocuments(keywords, 3, vectors, 2, 2)
	if got := searchDocumentIDs(secondPage.Hits); !reflect.DeepEqual(got, []string{"keyword-2", "keyword-3"}) {
		t.Fatalf("second page order = %v", got)
	}
	if firstPage.Total != 4 || secondPage.Total != 4 {
		t.Fatalf("totals = %d, %d; want 4", firstPage.Total, secondPage.Total)
	}
}

func TestFuseSearchDocumentsDegradesToKeywordRanking(t *testing.T) {
	t.Parallel()

	result := fuseSearchDocuments(
		[]SearchDocument{{DocumentID: "first"}, {DocumentID: "second"}},
		12,
		nil,
		1,
		10,
	)

	if got := searchDocumentIDs(result.Hits); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("document order = %v", got)
	}
	if result.Total != 12 {
		t.Fatalf("Total = %d, want 12", result.Total)
	}
	for _, doc := range result.Hits {
		if !reflect.DeepEqual(doc.MatchMethods, []string{"keyword"}) {
			t.Fatalf("match methods = %v", doc.MatchMethods)
		}
		if doc.Score <= 0 || doc.Score > 1 {
			t.Fatalf("score = %v, want (0, 1]", doc.Score)
		}
	}
}

func searchDocumentIDs(documents []SearchDocument) []string {
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.DocumentID)
	}
	return ids
}

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

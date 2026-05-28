package service

import "testing"

func TestMapMeilisearchHitUsesItemFingerprintAsDocumentID(t *testing.T) {
	t.Parallel()

	doc := mapMeilisearchHit(map[string]interface{}{
		"document_id":  "item-fingerprint",
		"asset_id":     "item-fingerprint",
		"content_hash": "content-sha256",
		"name":         "report.docx",
	})

	if doc.DocumentID != "item-fingerprint" {
		t.Fatalf("DocumentID = %q, want item fingerprint", doc.DocumentID)
	}
	if doc.AssetID != "item-fingerprint" {
		t.Fatalf("AssetID = %q, want item fingerprint", doc.AssetID)
	}
	if doc.FileName != "report.docx" {
		t.Fatalf("FileName = %q, want report.docx", doc.FileName)
	}
}

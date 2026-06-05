package scanflow

import (
	"testing"
	"time"
)

func TestNewScanResponse(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)
	resp := NewScanResponse("success", "done", ScanCounts{
		CatalogNodes: 1,
		Items:        2,
		Fields:       3,
		Extraction:   ExtractionCounts{Documents: 1, Extracted: 1, Indexed: 1},
	}, start, end)

	if resp.Status != "success" || resp.Message != "done" || resp.CatalogNodesScanned != 1 || resp.ItemsScanned != 2 || resp.FieldsScanned != 3 || resp.DurationMs != 2000 {
		t.Fatalf("response = %#v", resp)
	}
	if resp.Extraction == nil || resp.Extraction.Documents != 1 || resp.Extraction.Indexed != 1 {
		t.Fatalf("extraction = %#v", resp.Extraction)
	}
}

func TestScanResultMetadata(t *testing.T) {
	t.Parallel()

	metadata := ScanResultMetadata(ScanCounts{CatalogNodes: 1, Items: 2, Fields: 3})
	if metadata["catalog_nodes_scanned"] != 1 || metadata["items_scanned"] != 2 || metadata["fields_scanned"] != 3 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

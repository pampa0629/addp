package scantask

import (
	"testing"
	"time"
)

func TestNewScanResponse(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	completed := start.Add(250 * time.Millisecond)
	resp := NewScanResponse("success", "done", ScanCounts{Namespaces: 1, Items: 2, Fields: 3}, start, completed)

	if resp.DurationMs != 250 || resp.StartedAt != "2026-05-07 12:00:00" {
		t.Fatalf("response timing = %#v", resp)
	}
	if resp.NamespacesScanned != 1 || resp.ItemsScanned != 2 || resp.FieldsScanned != 3 {
		t.Fatalf("response counts = %#v", resp)
	}
}

func TestScanResultMetadata(t *testing.T) {
	t.Parallel()

	metadata := ScanResultMetadata(ScanCounts{Namespaces: 1, Items: 2, Fields: 3})
	if metadata["namespaces_scanned"] != 1 || metadata["items_scanned"] != 2 || metadata["fields_scanned"] != 3 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

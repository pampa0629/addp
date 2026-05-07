package scantask

import (
	"testing"
	"time"

	"github.com/addp/common/events"
	commonModels "github.com/addp/common/models"
)

func TestScanCompletedEventDetectsObjectStorage(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	event := ScanCompletedEvent(7, 3, commonModels.JSONMap{
		"objects_scanned": 4,
		"items_scanned":   2,
	}, ts)

	if event.ScanType != events.ScanTypeObjectStorage {
		t.Fatalf("scan type = %q", event.ScanType)
	}
	if event.ScannedItemsCount != 6 {
		t.Fatalf("items count = %d", event.ScannedItemsCount)
	}
	if !event.Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v", event.Timestamp)
	}
}

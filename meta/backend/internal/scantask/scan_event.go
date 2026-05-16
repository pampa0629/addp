package scantask

import (
	"time"

	"github.com/addp/common/events"
	commonModels "github.com/addp/common/models"
)

func ScanCompletedEvent(engineID, tenantID uint, summary commonModels.JSONMap, timestamp time.Time) events.ScanCompletedEvent {
	scannedItemsCount := 0
	scanType := events.ScanTypeDatabase

	if catalogNodesScanned, ok := summary["catalog_nodes_scanned"].(int); ok && catalogNodesScanned > 0 {
		scanType = events.ScanTypeDatabase
	}
	if objectsScanned, ok := summary["objects_scanned"].(int); ok && objectsScanned > 0 {
		scanType = events.ScanTypeObjectStorage
		scannedItemsCount = objectsScanned
	}
	if itemsScanned, ok := summary["items_scanned"].(int); ok {
		scannedItemsCount += itemsScanned
	}

	return events.ScanCompletedEvent{
		EngineID:          engineID,
		TenantID:          tenantID,
		ScanType:          scanType,
		ScannedNodes:      []string{},
		ScannedItemsCount: scannedItemsCount,
		Timestamp:         timestamp,
	}
}

package scantask

import (
	"fmt"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

type ScanCounts struct {
	Namespaces int
	Items      int
	Fields     int
}

func NewScanResponse(status, message string, counts ScanCounts, startTime, completedAt time.Time) *models.ScanResponse {
	return &models.ScanResponse{
		Status:            status,
		Message:           message,
		NamespacesScanned: counts.Namespaces,
		ItemsScanned:      counts.Items,
		FieldsScanned:     counts.Fields,
		DurationMs:        completedAt.Sub(startTime).Milliseconds(),
		StartedAt:         startTime.Format("2006-01-02 15:04:05"),
	}
}

func AutoScanResponse(engineCount int, counts ScanCounts, startTime, completedAt time.Time) *models.ScanResponse {
	return NewScanResponse("success", fmt.Sprintf("Successfully scanned %d engines", engineCount), counts, startTime, completedAt)
}

func ScanResultMetadata(counts ScanCounts) commonModels.JSONMap {
	return commonModels.JSONMap{
		"namespaces_scanned": counts.Namespaces,
		"items_scanned":      counts.Items,
		"fields_scanned":     counts.Fields,
	}
}

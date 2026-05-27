package scantask

import (
	"fmt"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

type ScanCounts struct {
	CatalogNodes int
	Items        int
	Fields       int
	Extraction   ExtractionCounts
}

type ExtractionCounts struct {
	Documents   int
	Extracted   int
	Unsupported int
	Failed      int
	Indexed     int
	IndexFailed int
}

func (c ExtractionCounts) Empty() bool {
	return c.Documents == 0 &&
		c.Extracted == 0 &&
		c.Unsupported == 0 &&
		c.Failed == 0 &&
		c.Indexed == 0 &&
		c.IndexFailed == 0
}

func NewScanResponse(status, message string, counts ScanCounts, startTime, completedAt time.Time) *models.ScanResponse {
	resp := &models.ScanResponse{
		Status:              status,
		Message:             message,
		CatalogNodesScanned: counts.CatalogNodes,
		ItemsScanned:        counts.Items,
		FieldsScanned:       counts.Fields,
		DurationMs:          completedAt.Sub(startTime).Milliseconds(),
		StartedAt:           startTime.Format("2006-01-02 15:04:05"),
	}
	if !counts.Extraction.Empty() {
		resp.Extraction = &models.ExtractionScanStats{
			Documents:   counts.Extraction.Documents,
			Extracted:   counts.Extraction.Extracted,
			Unsupported: counts.Extraction.Unsupported,
			Failed:      counts.Extraction.Failed,
			Indexed:     counts.Extraction.Indexed,
			IndexFailed: counts.Extraction.IndexFailed,
		}
	}
	return resp
}

func AutoScanResponse(engineCount int, counts ScanCounts, startTime, completedAt time.Time) *models.ScanResponse {
	return NewScanResponse("success", fmt.Sprintf("Successfully scanned %d engines", engineCount), counts, startTime, completedAt)
}

func ScanResultMetadata(counts ScanCounts) commonModels.JSONMap {
	return commonModels.JSONMap{
		"catalog_nodes_scanned": counts.CatalogNodes,
		"items_scanned":         counts.Items,
		"fields_scanned":        counts.Fields,
	}
}

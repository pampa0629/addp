package scanflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
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

func MergeExtractionCounts(left, right ExtractionCounts) ExtractionCounts {
	left.Documents += right.Documents
	left.Extracted += right.Extracted
	left.Unsupported += right.Unsupported
	left.Failed += right.Failed
	left.Indexed += right.Indexed
	left.IndexFailed += right.IndexFailed
	return left
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
		resp.Extraction = ExtractionStatsModel(counts.Extraction)
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

func ExtractionStatsModel(counts ExtractionCounts) *models.ExtractionScanStats {
	if counts.Empty() {
		return nil
	}
	return &models.ExtractionScanStats{
		Documents:   counts.Documents,
		Extracted:   counts.Extracted,
		Unsupported: counts.Unsupported,
		Failed:      counts.Failed,
		Indexed:     counts.Indexed,
		IndexFailed: counts.IndexFailed,
	}
}

func ScanResponseFromExecution(exec *commonExecution.TaskExecution) (*models.ScanResponse, error) {
	if exec == nil {
		return nil, errors.New("execution is nil")
	}
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		if msg := executionErrorMessage(exec); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, fmt.Errorf("execution completed with status %s", exec.Status)
	}

	resp := &models.ScanResponse{
		Status:              exec.Status,
		Message:             executionMessage(exec),
		CatalogNodesScanned: jsonMapInt(exec.Metadata, "catalog_nodes_scanned"),
		ItemsScanned:        jsonMapInt(exec.Metadata, "items_scanned"),
		FieldsScanned:       jsonMapInt(exec.Metadata, "fields_scanned"),
	}
	if exec.ExecutionTimeMs != nil {
		resp.DurationMs = *exec.ExecutionTimeMs
	}
	if exec.StartedAt != nil {
		resp.StartedAt = exec.StartedAt.Format(time.RFC3339)
	}
	if extraction := ExtractionStatsFromMetadata(exec.Metadata); extraction != nil {
		resp.Extraction = extraction
	}
	return resp, nil
}

func ExtractionStatsFromMetadata(metadata commonModels.JSONMap) *models.ExtractionScanStats {
	raw := metadata["extraction"]
	if raw == nil {
		return nil
	}
	extractionMap, ok := raw.(map[string]interface{})
	if !ok {
		if jsonMap, ok := raw.(commonModels.JSONMap); ok {
			extractionMap = map[string]interface{}(jsonMap)
		} else {
			return nil
		}
	}
	stats := &models.ExtractionScanStats{
		Documents:   intFromAny(extractionMap["documents"]),
		Extracted:   intFromAny(extractionMap["extracted"]),
		Unsupported: intFromAny(extractionMap["unsupported"]),
		Failed:      intFromAny(extractionMap["failed"]),
		Indexed:     intFromAny(extractionMap["indexed"]),
		IndexFailed: intFromAny(extractionMap["index_failed"]),
	}
	if stats.Documents == 0 && stats.Extracted == 0 && stats.Unsupported == 0 && stats.Failed == 0 && stats.Indexed == 0 && stats.IndexFailed == 0 {
		return nil
	}
	return stats
}

func executionMessage(exec *commonExecution.TaskExecution) string {
	if exec != nil && exec.CurrentStep != nil && strings.TrimSpace(*exec.CurrentStep) != "" {
		return *exec.CurrentStep
	}
	return "Scan completed successfully"
}

func executionErrorMessage(exec *commonExecution.TaskExecution) string {
	if exec == nil {
		return ""
	}
	if msg, ok := exec.ErrorDetails.GetString("message"); ok {
		return msg
	}
	if exec.CurrentStep != nil {
		return strings.TrimSpace(*exec.CurrentStep)
	}
	return ""
}

func jsonMapInt(m commonModels.JSONMap, key string) int {
	if m == nil {
		return 0
	}
	return intFromAny(m[key])
}

func intFromAny(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case json.Number:
		if i64, err := v.Int64(); err == nil {
			return int(i64)
		}
	case string:
		if v == "" {
			return 0
		}
		if i64, err := strconv.ParseInt(v, 10, 64); err == nil {
			return int(i64)
		}
	}
	return 0
}

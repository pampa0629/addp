package models

import (
	"encoding/json"

	commonExecution "github.com/addp/common/execution"
)

// ScanExecutionResponse exposes immutable observations, never the execution model.
type ScanExecutionResponse struct {
	*commonExecution.Observation
	TenantID    int                  `json:"tenant_id"`
	ScanContext ScanExecutionContext `json:"scan_context"`
}

type ScanExecutionContext struct {
	EngineID  uint   `json:"engine_id"`
	ScanDepth string `json:"scan_depth,omitempty"`
}

func NewScanExecutionResponse(item *commonExecution.TaskExecution) *ScanExecutionResponse {
	if item == nil {
		return nil
	}
	context := ScanExecutionContext{}
	if data, err := json.Marshal(item.ExecutionConfig); err == nil {
		var parsed ScanExecutionContext
		if json.Unmarshal(data, &parsed) == nil {
			context.EngineID = parsed.EngineID
			if parsed.ScanDepth == "basic" || parsed.ScanDepth == "deep" {
				context.ScanDepth = parsed.ScanDepth
			}
		}
	}
	return &ScanExecutionResponse{Observation: commonExecution.Observe(item), TenantID: item.TenantID, ScanContext: context}
}

func NewScanExecutionResponses(items []*commonExecution.TaskExecution) []*ScanExecutionResponse {
	result := make([]*ScanExecutionResponse, 0, len(items))
	for _, item := range items {
		result = append(result, NewScanExecutionResponse(item))
	}
	return result
}

// ScanExecutionListResponse uses the same safe projection as the detail.
type ScanExecutionListResponse struct {
	Items      []*ScanExecutionResponse `json:"items"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"page_size"`
	TotalPages int64                    `json:"total_pages"`
}

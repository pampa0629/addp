package api

import (
	"encoding/json"
	"fmt"

	"github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
)

// Quality owns professional results; neither Monitor nor TaskProvider uses this DTO.
type qualityExecutionResponse struct {
	*execution.Observation
	ExecutionConfig qualityExecutionConfigResponse `json:"execution_config"`
	Metadata        qualityExecutionMetadata       `json:"metadata"`
	Outputs         qualityExecutionOutputs        `json:"outputs"`
}

type qualityExecutionConfigResponse struct {
	TaskVersion   int64                     `json:"task_version,omitempty"`
	TargetKey     string                    `json:"target_key,omitempty"`
	TableBindings []models.PlanTableBinding `json:"table_bindings,omitempty"`
	// RawMessage preserves absent versus explicit null historical ownership.
	OwnerDomainID json.RawMessage `json:"owner_domain_id,omitempty" swaggertype:"integer"`
}

type qualityExecutionOutputs struct {
	Passed *bool `json:"passed,omitempty"`
}

type qualityExecutionMetadata struct {
	SchemaVersion string                      `json:"schema_version,omitempty"`
	Passed        *bool                       `json:"passed,omitempty"`
	QualityScore  *float64                    `json:"quality_score,omitempty"`
	Rules         []qualityRuleResultResponse `json:"rules,omitempty"`
	Outputs       qualityExecutionOutputs     `json:"outputs"`
}

type qualityRuleResultResponse struct {
	RuleKey     string              `json:"rule_key"`
	RuleID      int64               `json:"rule_id,omitempty"`
	RevisionNo  int64               `json:"revision_no,omitempty"`
	Type        string              `json:"type"`
	Severity    string              `json:"severity"`
	Passed      bool                `json:"passed"`
	Name        string              `json:"name,omitempty"`
	TotalCount  int64               `json:"total_count"`
	Table       string              `json:"table"`
	Columns     []string            `json:"columns"`
	FailedCount int64               `json:"failed_count"`
	Observed    qualityRuleObserved `json:"observed"`
}

type qualityRuleObserved struct {
	TotalCount *int64 `json:"total_count,omitempty"`
	RowCount   *int64 `json:"row_count,omitempty"`
}

func newQualityExecutionResponse(item *execution.TaskExecution) (*qualityExecutionResponse, error) {
	response := &qualityExecutionResponse{Observation: execution.Observe(item)}
	for _, projection := range []struct{ source, target interface{} }{
		{item.ExecutionConfig, &response.ExecutionConfig},
		{item.Metadata, &response.Metadata},
	} {
		data, err := json.Marshal(projection.source)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, projection.target); err != nil {
			return nil, err
		}
	}
	if raw := response.ExecutionConfig.OwnerDomainID; len(raw) != 0 {
		var domainID *int64
		if err := json.Unmarshal(raw, &domainID); err != nil || (domainID != nil && *domainID <= 0) {
			return nil, fmt.Errorf("invalid frozen Quality domain identity")
		}
	}
	response.Outputs = response.Metadata.Outputs
	return response, nil
}

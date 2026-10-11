package api

import (
	"encoding/json"
	"errors"
	execution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	orchi18n "github.com/addp/orchestrator/i18n"
	"github.com/addp/orchestrator/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"net/http"
)

// Orchestrator owns step orchestration facts, not child business results.
type orchestrationExecutionResponse struct {
	*execution.Observation
	Metadata orchestrationExecutionMetadata `json:"metadata"`
}

type orchestrationExecutionMetadata struct {
	StepResults map[string]orchestrationStepResponse `json:"step_results"`
}

type orchestrationStepResponse struct {
	execution.DiagnosticStep
	Error  string                       `json:"error,omitempty"`
	Result *orchestrationChildReference `json:"result,omitempty"`
}

type orchestrationChildReference struct {
	ExecutionID string `json:"execution_id"`
}

type orchestrationExecutionListResponse struct {
	Data       []*orchestrationExecutionResponse `json:"data"`
	Total      int64                             `json:"total"`
	Page       int                               `json:"page"`
	PageSize   int                               `json:"page_size"`
	TotalPages int                               `json:"total_pages"`
}

// Project first, then localize closed errors; never copy internal JSON maps.
func localizeExecutionFailure(c *gin.Context, item *execution.TaskExecution) *orchestrationExecutionResponse {
	if item == nil {
		return nil
	}
	response := &orchestrationExecutionResponse{Observation: execution.Observe(item),
		Metadata: orchestrationExecutionMetadata{StepResults: map[string]orchestrationStepResponse{}}}
	if code, ok := response.ErrorDetails["code"].(string); ok {
		if message, known := orchi18n.ExecutionFailureMessage(commoni18n.GetLang(c), code); known {
			response.ErrorDetails["message"] = message
		}
	}
	// RawMessage is confined to reading stored evidence, never serialized out.
	recorded := map[string]json.RawMessage{}
	if data, err := json.Marshal(item.Metadata["step_results"]); err == nil {
		_ = json.Unmarshal(data, &recorded)
	}
	for _, safe := range response.Steps {
		step := orchestrationStepResponse{DiagnosticStep: safe}
		var facts struct {
			ErrorCode string `json:"error_code"`
			Result    struct {
				ExecutionID string `json:"execution_id"`
			} `json:"result"`
		}
		if json.Unmarshal(recorded[safe.ID], &facts) == nil {
			if message, known := orchi18n.ExecutionFailureMessage(commoni18n.GetLang(c), facts.ErrorCode); known {
				step.ErrorCode, step.Error = facts.ErrorCode, message
			}
			if id, err := uuid.Parse(facts.Result.ExecutionID); err == nil && id != uuid.Nil && id.String() == facts.Result.ExecutionID {
				step.Result = &orchestrationChildReference{ExecutionID: id.String()}
			}
		}
		response.Metadata.StepResults[safe.ID] = step
	}
	return response
}

func localizedExecutionList(c *gin.Context, items []*execution.TaskExecution) []*orchestrationExecutionResponse {
	result := make([]*orchestrationExecutionResponse, len(items))
	for i, item := range items {
		result[i] = localizeExecutionFailure(c, item)
	}
	return result
}

func respondExecutionAdmissionError(c *gin.Context, err error) {
	var stepError *models.StepValidationError
	if errors.As(err, &stepError) {
		respondOrchestrationValidationError(c, err)
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, "orchestrator.error.execution_admission_failed")})
}

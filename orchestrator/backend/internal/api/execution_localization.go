package api

import (
	"errors"
	execution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	orchi18n "github.com/addp/orchestrator/i18n"
	"github.com/addp/orchestrator/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
)

// Localize only the closed failure catalog, leaving private outputs and stored facts intact.
func localizeExecutionFailure(c *gin.Context, item *execution.TaskExecution) *execution.TaskExecution {
	if item == nil {
		return nil
	}
	copyItem := *item
	copyItem.ErrorDetails = localizedFailureObject(c, item.ErrorDetails, "code", "message")
	if steps, ok := item.Metadata["step_results"].(map[string]interface{}); ok {
		metadata := commonModels.JSONMap{}
		for key, value := range item.Metadata {
			metadata[key] = value
		}
		results := map[string]interface{}{}
		for id, value := range steps {
			if step, ok := value.(map[string]interface{}); ok {
				results[id] = localizedFailureObject(c, step, "error_code", "error")
			} else {
				results[id] = value
			}
		}
		metadata["step_results"] = results
		copyItem.Metadata = metadata
	}
	return &copyItem
}

func localizedFailureObject(c *gin.Context, value map[string]interface{}, codeKey, messageKey string) commonModels.JSONMap {
	if value == nil {
		return nil
	}
	copyValue := commonModels.JSONMap{}
	for key, item := range value {
		copyValue[key] = item
	}
	if code, ok := value[codeKey].(string); ok {
		if message, known := orchi18n.ExecutionFailureMessage(commoni18n.GetLang(c), code); known {
			copyValue[messageKey] = message
		}
	}
	return copyValue
}

func localizedExecutionList(c *gin.Context, items []*execution.TaskExecution) []*execution.TaskExecution {
	result := make([]*execution.TaskExecution, len(items))
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

package api

import (
	"fmt"
	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/taskprovider"
	qualityi18n "github.com/addp/quality/i18n"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ExecutionHandler struct {
	executionRepo *commonExecution.TaskExecutionRepository
}

func qualityExecutionFilter(tenantID, page, pageSize int) commonExecution.TaskExecutionFilter {
	return commonExecution.TaskExecutionFilter{
		TenantID: tenantID,
		Module:   commonExecution.ModuleQuality,
		Page:     page,
		PageSize: pageSize,
	}
}

func qualityExecutionStatus(value string) (string, error) {
	switch value {
	case "", commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning,
		commonExecution.ExecutionStatusSuccess, commonExecution.ExecutionStatusFailed,
		commonExecution.ExecutionStatusTimeout, commonExecution.ExecutionStatusCancelled:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported execution status %q", value)
	}
}

func isQualityExecution(item *commonExecution.TaskExecution) bool {
	return item != nil && item.Module == commonExecution.ModuleQuality && (item.TaskType == commonExecution.TaskTypeQualityCheck || item.TaskType == commonExecution.TaskTypeMaterializationGate)
}

func NewExecutionHandler(executionRepo *commonExecution.TaskExecutionRepository) *ExecutionHandler {
	return &ExecutionHandler{executionRepo: executionRepo}
}

// @Summary 获取执行记录列表 | List execution records
// @Tags Execution
// @Produce json
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20) maximum(100)
// @Param status query string false "执行状态：pending|running|success|failed|timeout|cancelled | Execution status"
// @Success 200 {object} qualityExecutionListResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.task_provider.read"]
// @Router /executions [get]
// @Security BearerAuth
func (h *ExecutionHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	page := 1
	pageSize := 20
	page, pageSize = pageParams(c.Query("page"), c.Query("page_size"))
	status, err := qualityExecutionStatus(c.Query("status"))
	if err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}

	filter := qualityExecutionFilter(int(tenantID), page, pageSize)
	filter.Status = status
	items, total, err := h.executionRepo.List(c.Request.Context(), filter)
	if err != nil {
		respondQualityError(c, http.StatusInternalServerError, "execution_list_failed", commoni18n.T(c, qualityi18n.MsgExecutionListFailed))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":        items,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages(total, pageSize),
	})
}

// @Summary 获取执行记录详情 | Get execution record detail
// @Tags Execution
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} taskprovider.ExecutionStatusResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.task_provider.read"]
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *ExecutionHandler) Get(c *gin.Context) {
	tenantID := getTenantID(c)
	executionID := c.Param("execution_id")

	item, err := h.executionRepo.GetByExecutionID(c.Request.Context(), executionID, int(tenantID))
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgExecutionNotFound, qualityi18n.MsgExecutionListFailed)
		return
	}
	if !isQualityExecution(item) {
		respondQualityError(c, http.StatusNotFound, "execution_not_found", commoni18n.T(c, qualityi18n.MsgExecutionNotFound))
		return
	}

	c.JSON(http.StatusOK, taskprovider.NewExecutionStatusResponse(item))
}

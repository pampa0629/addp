package api

import (
	commonExecution "github.com/addp/common/execution"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ExecutionHandler struct {
	executionRepo *commonExecution.TaskExecutionRepository
}

func NewExecutionHandler(executionRepo *commonExecution.TaskExecutionRepository) *ExecutionHandler {
	return &ExecutionHandler{executionRepo: executionRepo}
}

// @Summary 获取执行记录列表 | List execution records
// @Tags Execution
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /executions [get]
// @Security BearerAuth
func (h *ExecutionHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	page := 1
	pageSize := 20

	items, total, err := h.executionRepo.List(c.Request.Context(), commonExecution.TaskExecutionFilter{
		TenantID: int(tenantID),
		Module:   commonExecution.ModuleQuality,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	c.JSON(http.StatusOK, gin.H{
		"data":        items,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// @Summary 获取执行记录详情 | Get execution record detail
// @Tags Execution
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{}
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *ExecutionHandler) Get(c *gin.Context) {
	tenantID := getTenantID(c)
	executionID := c.Param("execution_id")

	item, err := h.executionRepo.GetByExecutionID(c.Request.Context(), executionID, int(tenantID))
	if err != nil || item.Module != commonExecution.ModuleQuality {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution not found"})
		return
	}

	c.JSON(http.StatusOK, item)
}

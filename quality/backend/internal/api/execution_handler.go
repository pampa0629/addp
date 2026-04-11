package api

import (
	"net/http"

	commonModels "github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ExecutionHandler struct {
	db *gorm.DB
}

func NewExecutionHandler(db *gorm.DB) *ExecutionHandler {
	return &ExecutionHandler{db: db}
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

	var total int64
	var items []commonModels.TaskExecution

	q := h.db.Table("common.task_executions").
		Where("tenant_id = ? AND module = ?", tenantID, commonModels.ModuleQuality).
		Order("created_at DESC")

	q.Count(&total)
	if err := q.Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
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
// @Param id path string true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{}
// @Router /executions/{id} [get]
// @Security BearerAuth
func (h *ExecutionHandler) Get(c *gin.Context) {
	tenantID := getTenantID(c)
	executionID := c.Param("id")

	var item commonModels.TaskExecution
	if err := h.db.Table("common.task_executions").
		Where("execution_id = ? AND tenant_id = ? AND module = ?", executionID, tenantID, commonModels.ModuleQuality).
		First(&item).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution not found"})
		return
	}

	c.JSON(http.StatusOK, item)
}

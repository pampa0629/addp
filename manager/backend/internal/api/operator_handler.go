package api

import (
	"net/http"

	"github.com/addp/common/models"
	"github.com/addp/manager/internal/service"

	"github.com/gin-gonic/gin"
)

// OperatorHandler 算子API处理器
type OperatorHandler struct {
	operatorService *service.OperatorService
}

// NewOperatorHandler 创建算子处理器
func NewOperatorHandler(operatorService *service.OperatorService) *OperatorHandler {
	return &OperatorHandler{
		operatorService: operatorService,
	}
}

// ListOperators GET /api/manager/operators
func (h *OperatorHandler) ListOperators(c *gin.Context) {
	operators := h.operatorService.GetOperators()

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"operators": operators,
		"count":     len(operators),
	})
}

// ExecuteOperator POST /api/manager/operators/:name/execute
func (h *OperatorHandler) ExecuteOperator(c *gin.Context) {
	operatorName := c.Param("name")
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	var req models.OperatorExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	// 执行算子
	result, err := h.operatorService.ExecuteOperator(
		c.Request.Context(),
		operatorName,
		tenantID,
		userID,
		req.Params,
		req.ExecuteNow,
		req.TaskName,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetTaskStatus GET /api/manager/operators/tasks/:task_id
func (h *OperatorHandler) GetTaskStatus(c *gin.Context) {
	taskID := c.Param("task_id")

	task, err := h.operatorService.GetTaskStatus(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "success",
		"task_id":    task.TaskID,
		"task_status": string(task.Status),
		"message":    task.Message,
		"progress":   task.Progress,
		"start_time": task.StartTime,
		"end_time":   task.EndTime,
		"result":     task.Result,
		"error":      task.Error,
	})
}

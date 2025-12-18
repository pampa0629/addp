package api

import (
	"net/http"

	"github.com/addp/common/models"
	"github.com/addp/meta/internal/service"

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

// ListOperators GET /api/meta/operators
func (h *OperatorHandler) ListOperators(c *gin.Context) {
	operators := h.operatorService.GetOperators()

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"operators": operators,
		"count":     len(operators),
	})
}

// ExecuteOperator POST /api/meta/operators/:name/execute
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

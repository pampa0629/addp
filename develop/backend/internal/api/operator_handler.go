package api

import (
	"net/http"
	"strconv"

	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// OperatorHandler 算子发现API处理器
type OperatorHandler struct {
	operatorDiscovery *service.OperatorDiscoveryService
}

type OperatorListResponse struct {
	Status           string                             `json:"status"`
	WorkflowEngineID uint64                             `json:"workflow_engine_id"`
	Operators        []service.PublicOperatorDescriptor `json:"operators"`
	Count            int                                `json:"count"`
}

// NewOperatorHandler 创建算子处理器
func NewOperatorHandler(operatorDiscovery *service.OperatorDiscoveryService) *OperatorHandler {
	return &OperatorHandler{
		operatorDiscovery: operatorDiscovery,
	}
}

// ListOperatorsByWorkflowEngine 获取指定工作流引擎实例的算子
// @Summary 根据工作流引擎实例获取算子列表 | List operators by workflow engine instance
// @Tags Operator
// @Produce json
// @Param id path int true "工作流引擎实例ID | Workflow engine instance ID"
// @Success 200 {object} OperatorListResponse "算子列表 | Operator list"
// @Router /workflow-engines/{id}/operators [get]
func (h *OperatorHandler) ListOperatorsByWorkflowEngine(c *gin.Context) {
	workflowEngineID64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || workflowEngineID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_engine_id 必须是正整数"})
		return
	}

	operators, err := h.operatorDiscovery.GetOperatorsByWorkflowEngineID(c.Request.Context(), uint(workflowEngineID64))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, OperatorListResponse{
		Status:           "success",
		WorkflowEngineID: workflowEngineID64,
		Operators:        operators,
		Count:            len(operators),
	})
}

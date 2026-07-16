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

type WorkflowValidationRequest struct {
	WorkflowEngineID   uint                   `json:"workflow_engine_id" binding:"required"`
	WorkflowDefinition map[string]interface{} `json:"workflow_definition" binding:"required"`
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
// @Security BearerAuth
// @Param id path int true "工作流引擎实例ID | Workflow engine instance ID"
// @Success 200 {object} OperatorListResponse "算子列表 | Operator list"
// @Router /workflow-engines/{id}/operators [get]
func (h *OperatorHandler) ListOperatorsByWorkflowEngine(c *gin.Context) {
	workflowEngineID64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || workflowEngineID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_engine_id 必须是正整数"})
		return
	}

	operators, err := h.operatorDiscovery.GetOperatorsByWorkflowEngineIDForTenant(
		c.Request.Context(),
		uint(workflowEngineID64),
		c.GetUint("tenant_id"),
	)
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

// ValidateWorkflow 校验候选工作流，不创建 execution。
// @Summary 校验工作流定义 | Validate workflow definition
// @Description 按 addp.workflow/v1 基础结构和目标运行时 Public Operator Spec 校验候选工作流，不创建 execution | Validate a workflow candidate against addp.workflow/v1 and the target runtime Public Operator Spec without creating an execution
// @Tags Operator
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body WorkflowValidationRequest true "工作流校验请求 | Workflow validation request"
// @Success 200 {object} service.WorkflowValidationResult "校验结果 | Validation result"
// @Failure 400 {object} map[string]interface{} "请求或工作流引擎错误 | Invalid request or workflow engine"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Router /workflow-validations [post]
func (h *OperatorHandler) ValidateWorkflow(c *gin.Context) {
	var request WorkflowValidationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_engine_id 和 workflow_definition 为必填字段"})
		return
	}
	if request.WorkflowEngineID == 0 || len(request.WorkflowDefinition) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_engine_id 必须为正整数，workflow_definition 不能为空"})
		return
	}

	result, err := h.operatorDiscovery.ValidateWorkflowForTenant(
		c.Request.Context(),
		request.WorkflowEngineID,
		request.WorkflowDefinition,
		c.GetUint("tenant_id"),
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

package api

import (
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

type ToolApprovalHandler struct {
	service *service.ToolApprovalService
}

func NewToolApprovalHandler(approvalService *service.ToolApprovalService) *ToolApprovalHandler {
	return &ToolApprovalHandler{service: approvalService}
}

// GetApproval 查询委托 Tool 审批。
// @Summary 查询 Tool 审批 | Get Tool approval
// @Description 仅原申请用户可读取审批状态和最小请求摘要，不返回完整 workflow payload。| Only the requesting user can read the approval status and minimal request summary; the full workflow payload is not returned.
// @Tags Tool Approval
// @Produce json
// @Param id path string true "审批 ID | Approval ID"
// @Success 200 {object} models.ToolApprovalResponse "审批详情 | Approval details"
// @Failure 403 {object} models.ToolApprovalErrorResponse "无权访问审批 | Approval access denied"
// @Failure 404 {object} models.ToolApprovalErrorResponse "审批不存在 | Approval not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /approvals/{id} [get]
func (handler *ToolApprovalHandler) GetApproval(c *gin.Context) {
	authContext, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		writeToolApprovalError(c, serviceError("approval_forbidden", "缺少认证上下文"))
		return
	}
	approval, err := handler.service.GetApproval(c.Request.Context(), authContext, c.Param("id"))
	if err != nil {
		writeToolApprovalError(c, err)
		return
	}
	c.JSON(http.StatusOK, models.NewToolApprovalResponse(approval))
}

// DecideApproval 决定委托 Tool 审批。
// @Summary 决定 Tool 审批 | Decide Tool approval
// @Description 仅原申请用户使用第一方或 OAuth User Access Token 提交 approved 或 rejected；委托令牌和内部身份不能作出决定。| Only the requesting user may submit approved or rejected with a first-party or OAuth user access token; delegated tokens and internal identities cannot decide.
// @Tags Tool Approval
// @Accept json
// @Produce json
// @Param id path string true "审批 ID | Approval ID"
// @Param body body models.ToolApprovalDecisionRequest true "审批决定 | Approval decision"
// @Success 200 {object} models.ToolApprovalResponse "审批详情 | Approval details"
// @Failure 400 {object} models.ToolApprovalErrorResponse "决定无效 | Invalid decision"
// @Failure 403 {object} models.ToolApprovalErrorResponse "无权决定审批 | Approval decision denied"
// @Failure 404 {object} models.ToolApprovalErrorResponse "审批不存在 | Approval not found"
// @Failure 409 {object} models.ToolApprovalErrorResponse "审批已处理 | Approval already decided"
// @Failure 410 {object} models.ToolApprovalErrorResponse "审批已过期 | Approval expired"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute"]
// @Router /approvals/{id}/decision [post]
func (handler *ToolApprovalHandler) DecideApproval(c *gin.Context) {
	var req models.ToolApprovalDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeToolApprovalError(c, serviceError("approval_invalid_decision", err.Error()))
		return
	}
	authContext, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		writeToolApprovalError(c, serviceError("approval_forbidden", "缺少认证上下文"))
		return
	}
	approval, err := handler.service.DecideApproval(
		c.Request.Context(),
		authContext,
		c.Param("id"),
		req.Decision,
	)
	if err != nil {
		writeToolApprovalError(c, err)
		return
	}
	c.JSON(http.StatusOK, models.NewToolApprovalResponse(approval))
}

package api

import (
	"errors"
	commonAPI "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	i18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

// GetRunApproval reads the original user's one-shot run approval.
// @Summary 查询运行审批 | Get run approval
// @Description 仅原租户用户本人可读；委托令牌、Service 和不同成员关系不可读取。| Only the original tenant user may read; delegated tokens, services and other memberships are denied.
// @Tags Task Approval
// @Produce json
// @Param id path string true "审批 ID | Approval ID"
// @Success 200 {object} models.RunApproval
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /run-approvals/{id} [get]
// @Security BearerAuth
func (h *TaskHandler) GetRunApproval(c *gin.Context) {
	a, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		respondRunApprovalError(c, &service.RunApprovalError{Code: "approval_forbidden"})
		return
	}
	item, err := h.taskService.GetRunApproval(c.Request.Context(), a, c.Param("id"))
	if err != nil {
		respondRunApprovalError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// DecideRunApproval records an explicit click from the Agent UI.
// @Summary 确认或拒绝任务运行 | Approve or reject task run
// @Description 用户本人显式提交当前指纹的批准或拒绝；仅保存决定，不启动任务。委托令牌不能批准。| The user explicitly approves or rejects the displayed fingerprint. Records the decision only, without starting execution; delegated tokens cannot approve.
// @Tags Task Approval
// @Accept json
// @Produce json
// @Param id path string true "审批 ID | Approval ID"
// @Param request body models.RunApprovalDecision true "运行决定 | Run decision"
// @Success 200 {object} models.RunApproval
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 410 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.execute"]
// @Router /run-approvals/{id}/decision [post]
// @Security BearerAuth
func (h *TaskHandler) DecideRunApproval(c *gin.Context) {
	var req models.RunApprovalDecision
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		respondRunApprovalError(c, &service.RunApprovalError{Code: "approval_invalid_decision"})
		return
	}
	a, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		respondRunApprovalError(c, &service.RunApprovalError{Code: "approval_forbidden"})
		return
	}
	item, err := h.taskService.DecideRunApproval(c.Request.Context(), a, c.Param("id"), req)
	if err != nil {
		respondRunApprovalError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func respondRunApprovalError(c *gin.Context, err error) {
	var failure *service.RunApprovalError
	if !errors.As(err, &failure) {
		respondTaskServiceError(c, err)
		return
	}
	status := http.StatusConflict
	message := "transfer." + failure.Code
	switch failure.Code {
	case "approval_forbidden":
		status = http.StatusForbidden
	case "approval_not_found":
		status = http.StatusNotFound
	case "approval_invalid_request", "approval_invalid_decision":
		status = http.StatusBadRequest
	case "approval_expired":
		status = http.StatusGone
	}
	c.JSON(status, gin.H{"error_code": failure.Code, "error": i18n.T(c, message)})
}

// handleDelegatedTaskStart never starts from model text or an empty request.
// The first request only creates the owner review; resume consumes that review.
func (h *TaskHandler) handleDelegatedTaskStart(c *gin.Context, id uint) {
	a, _ := commonAuth.AuthContextFromGin(c)
	var req models.StartTaskRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		respondRunApprovalError(c, &service.RunApprovalError{Code: "approval_invalid_request"})
		return
	}
	if req.ApprovalID == "" && req.RequestFingerprint == "" {
		item, err := h.taskService.CreateRunApproval(c.Request.Context(), a, id)
		if err != nil {
			respondRunApprovalError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, models.RunApprovalRequired{Status: "approval_required", InteractionID: item.ID.String(), RequestFingerprint: item.RequestFingerprint, RequestSummary: item.RequestSummary, ExpiresAt: item.ExpiresAt})
		return
	}
	if req.ApprovalID == "" || req.RequestFingerprint == "" {
		respondRunApprovalError(c, &service.RunApprovalError{Code: "approval_invalid_request"})
		return
	}
	execution, err := h.taskService.StartApprovedTask(transferTaskAuthorizationContext(c), a, id, req)
	if err != nil {
		respondRunApprovalError(c, err)
		return
	}
	respondExecution(c, http.StatusOK, execution)
}

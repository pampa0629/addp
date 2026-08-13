package api

import (
	"net/http"

	commonAPI "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type IssueHandler struct {
	svc *service.IssueService
}

func NewIssueHandler(svc *service.IssueService) *IssueHandler {
	return &IssueHandler{svc: svc}
}

// @Summary 获取问题工单列表 | List quality issues
// @Tags Issue
// @Produce json
// @Param status query string false "状态 | Status" Enums(open,resolved,ignored)
// @Param engine_id query int false "引擎ID | Engine ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20) maximum(100)
// @Success 200 {object} qualityIssueListResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.issue.read"]
// @Router /issues [get]
// @Security BearerAuth
func (h *IssueHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	status := c.Query("status")
	if status != "" && status != "open" && status != "resolved" && status != "ignored" {
		respondInvalidRequest(c, "")
		return
	}
	engineID, err := optionalPositiveID(c.Query("engine_id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}

	page, pageSize := pageParams(c.Query("page"), c.Query("page_size"))
	items, total, err := h.svc.List(tenantID, status, engineID, page, pageSize)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages(total, pageSize)})
}

// @Summary 获取问题工单详情 | Get issue detail
// @Tags Issue
// @Produce json
// @Param id path int true "工单ID | Issue ID"
// @Success 200 {object} qualityIssueResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.issue.read"]
// @Router /issues/{id} [get]
// @Security BearerAuth
func (h *IssueHandler) Get(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	item, err := h.svc.Get(id, tenantID)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgIssueNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 更新问题工单状态 | Update issue status
// @Tags Issue
// @Accept json
// @Produce json
// @Param id path int true "工单ID | Issue ID"
// @Param body body issueStatusRequest true "状态信息（必须包含处理说明） | Status and required note"
// @Success 200 {object} qualityMessageResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.issue.update"]
// @Router /issues/{id}/status [put]
// @Security BearerAuth
func (h *IssueHandler) UpdateStatus(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var body issueStatusRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &body); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), id, tenantID, getUserID(c), body.Status, body.Note); err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgIssueNotFound, qualityi18n.MsgIssueUpdateFailed)
		return
	}
	c.JSON(http.StatusOK, qualityMessageResponse{Message: commoni18n.T(c, qualityi18n.MsgUpdated)})
}

package api

import (
	"net/http"
	"strconv"

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
// @Param status query string false "状态 | Status"
// @Param engine_id query int false "引擎ID | Engine ID"
// @Success 200 {array} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.issue.read"]
// @Router /issues [get]
// @Security BearerAuth
func (h *IssueHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	status := c.Query("status")
	engineID, _ := strconv.ParseInt(c.Query("engine_id"), 10, 64)

	items, err := h.svc.List(tenantID, status, engineID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

// @Summary 获取问题工单详情 | Get issue detail
// @Tags Issue
// @Produce json
// @Param id path int true "工单ID | Issue ID"
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.issue.read"]
// @Router /issues/{id} [get]
// @Security BearerAuth
func (h *IssueHandler) Get(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	item, err := h.svc.Get(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 更新问题工单状态 | Update issue status
// @Tags Issue
// @Accept json
// @Produce json
// @Param id path int true "工单ID | Issue ID"
// @Param body body map[string]string true "状态信息 | Status info"
// @Success 200 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.issue.update"]
// @Router /issues/{id}/status [put]
// @Security BearerAuth
func (h *IssueHandler) UpdateStatus(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateStatus(id, tenantID, body.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "updated"})
}

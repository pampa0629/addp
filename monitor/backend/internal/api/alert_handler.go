package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	moni18n "github.com/addp/monitor/i18n"
	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type AlertHandler struct{ service *service.AlertService }

type AlertIncidentResponse = monitorModels.AlertIncident

type ErrorResponse struct {
	Error string `json:"error" example:"告警不存在或已经恢复"`
}

func NewAlertHandler(alertService *service.AlertService) *AlertHandler {
	return &AlertHandler{service: alertService}
}

type SuppressAlertRequest struct {
	SuppressedUntil time.Time `json:"suppressed_until" binding:"required" example:"2026-07-16T08:00:00Z"`
}

// ListAlerts 查询告警事件
// @Summary 查询告警事件 | List alert incidents
// @Tags Monitor Alerts
// @Produce json
// @Param status query string false "告警状态 | Alert status"
// @Param severity query string false "严重级别 | Severity"
// @Param module query string false "模块 | Module"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} service.ListAlertsResponse "告警列表 | Alert list"
// @Router /alerts [get]
// @Security BearerAuth
func (h *AlertHandler) ListAlerts(c *gin.Context) {
	tenantID, ok := alertTenantID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := h.service.List(c.Request.Context(), service.ListAlertsRequest{
		TenantID: tenantID, Status: c.Query("status"), Severity: c.Query("severity"), Module: c.Query("module"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// AcknowledgeAlert 确认告警
// @Summary 确认告警 | Acknowledge alert
// @Tags Monitor Alerts
// @Produce json
// @Param id path int true "告警 ID | Alert ID"
// @Success 200 {object} AlertIncidentResponse "已确认告警 | Acknowledged alert"
// @Failure 404 {object} ErrorResponse
// @Router /alerts/{id}/acknowledge [post]
// @Security BearerAuth
func (h *AlertHandler) AcknowledgeAlert(c *gin.Context) {
	id, tenantID, ok := alertIdentity(c)
	if !ok {
		return
	}
	actor, _ := c.Get(commonAuth.ContextUsernameKey)
	alert, err := h.service.Acknowledge(c.Request.Context(), id, tenantID, fmt.Sprint(actor), time.Now())
	if errors.Is(err, service.ErrAlertNotActive) {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgAlertNotActive)})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, alert)
}

// SuppressAlert 抑制告警通知
// @Summary 抑制告警通知 | Suppress alert notifications
// @Tags Monitor Alerts
// @Accept json
// @Produce json
// @Param id path int true "告警 ID | Alert ID"
// @Param request body SuppressAlertRequest true "抑制截止时间 | Suppression deadline"
// @Success 200 {object} AlertIncidentResponse "已抑制告警 | Suppressed alert"
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /alerts/{id}/suppress [post]
// @Security BearerAuth
func (h *AlertHandler) SuppressAlert(c *gin.Context) {
	id, tenantID, ok := alertIdentity(c)
	if !ok {
		return
	}
	var request SuppressAlertRequest
	if err := c.ShouldBindJSON(&request); err != nil || !request.SuppressedUntil.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidSuppression)})
		return
	}
	alert, err := h.service.Suppress(c.Request.Context(), id, tenantID, request.SuppressedUntil)
	if errors.Is(err, service.ErrAlertNotActive) {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgAlertNotActive)})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, alert)
}

func alertIdentity(c *gin.Context) (uint, int, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidAlertID)})
		return 0, 0, false
	}
	tenantID, ok := alertTenantID(c)
	return uint(id), tenantID, ok
}

func alertTenantID(c *gin.Context) (int, bool) {
	value, exists := c.Get(commonAuth.ContextTenantIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, moni18n.MsgTenantNotFound)})
		return 0, false
	}
	tenantID, ok := value.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, moni18n.MsgTenantNotFound)})
		return 0, false
	}
	return int(tenantID), true
}

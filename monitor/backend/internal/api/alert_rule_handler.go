package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
	moni18n "github.com/addp/monitor/i18n"
	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type AlertRuleHandler struct{ service *service.AlertRuleService }

type AlertRuleResponse = monitorModels.AlertRule

type CreateAlertRuleRequest struct {
	Name             string                        `json:"name" binding:"required" example:"核心同步任务连续失败"`
	Module           string                        `json:"module" binding:"required" example:"transfer"`
	TaskType         string                        `json:"task_type" binding:"required" example:"sync"`
	SourceTaskID     string                        `json:"source_task_id" binding:"required" example:"43"`
	SourceTaskName   string                        `json:"source_task_name" example:"生产数据同步"`
	RuleType         string                        `json:"rule_type" binding:"required" enums:"last_terminal_failed,last_terminal_timeout,consecutive_failures"`
	FailureThreshold int                           `json:"failure_threshold" example:"3"`
	Severity         string                        `json:"severity" binding:"required" enums:"warning,critical"`
	Enabled          *bool                         `json:"enabled" binding:"required"`
	Routes           []service.AlertRuleRouteInput `json:"routes"`
}

type UpdateAlertRuleRequest struct {
	Name             *string                        `json:"name,omitempty"`
	Module           *string                        `json:"module,omitempty"`
	TaskType         *string                        `json:"task_type,omitempty"`
	SourceTaskID     *string                        `json:"source_task_id,omitempty"`
	SourceTaskName   *string                        `json:"source_task_name,omitempty"`
	RuleType         *string                        `json:"rule_type,omitempty" enums:"last_terminal_failed,last_terminal_timeout,consecutive_failures"`
	FailureThreshold *int                           `json:"failure_threshold,omitempty"`
	Severity         *string                        `json:"severity,omitempty" enums:"warning,critical"`
	Enabled          *bool                          `json:"enabled,omitempty"`
	Routes           *[]service.AlertRuleRouteInput `json:"routes,omitempty"`
}

type DeleteAlertRuleResponse struct {
	Message string `json:"message" example:"告警规则已删除"`
}

func NewAlertRuleHandler(alertRuleService *service.AlertRuleService) *AlertRuleHandler {
	return &AlertRuleHandler{service: alertRuleService}
}

// ListAlertRuleTargets 查询可配置告警的任务
// @Summary 查询可配置告警的任务 | List alert rule targets
// @Tags Monitor Alert Rules
// @Produce json
// @Success 200 {array} service.AlertRuleTarget "任务列表 | Task list"
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.alert_rule.read"]
// @Router /alert-rule-targets [get]
// @Security BearerAuth
func (h *AlertRuleHandler) ListAlertRuleTargets(c *gin.Context) {
	tenantID, ok := alertTenantID(c)
	if !ok {
		return
	}
	targets, err := h.service.ListTargets(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, moni18n.MsgAlertRuleOperationFailed)})
		return
	}
	c.JSON(http.StatusOK, targets)
}

// ListAlertRules 查询告警规则
// @Summary 查询告警规则 | List alert rules
// @Tags Monitor Alert Rules
// @Produce json
// @Success 200 {array} AlertRuleResponse "告警规则列表 | Alert rule list"
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.alert_rule.read"]
// @Router /alert-rules [get]
// @Security BearerAuth
func (h *AlertRuleHandler) ListAlertRules(c *gin.Context) {
	tenantID, ok := alertTenantID(c)
	if !ok {
		return
	}
	rules, err := h.service.List(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, moni18n.MsgAlertRuleOperationFailed)})
		return
	}
	c.JSON(http.StatusOK, rules)
}

// CreateAlertRule 创建告警规则
// @Summary 创建告警规则 | Create alert rule
// @Tags Monitor Alert Rules
// @Accept json
// @Produce json
// @Param request body CreateAlertRuleRequest true "告警规则 | Alert rule"
// @Success 201 {object} AlertRuleResponse "已创建规则 | Created rule"
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.alert_rule.create"]
// @Router /alert-rules [post]
// @Security BearerAuth
func (h *AlertRuleHandler) CreateAlertRule(c *gin.Context) {
	tenantID, ok := alertTenantID(c)
	if !ok {
		return
	}
	var request CreateAlertRuleRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidAlertRule)})
		return
	}
	rule, err := h.service.Create(c.Request.Context(), service.CreateAlertRuleInput{
		TenantID: tenantID, Name: request.Name, Module: request.Module, TaskType: request.TaskType,
		SourceTaskID: request.SourceTaskID, SourceTaskName: request.SourceTaskName, RuleType: request.RuleType,
		FailureThreshold: request.FailureThreshold, Severity: request.Severity, Enabled: *request.Enabled, Routes: request.Routes,
	})
	if handleAlertRuleError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, rule)
}

// UpdateAlertRule 更新告警规则
// @Summary 更新告警规则 | Update alert rule
// @Tags Monitor Alert Rules
// @Accept json
// @Produce json
// @Param id path int true "规则 ID | Rule ID"
// @Param request body UpdateAlertRuleRequest true "规则变更 | Rule changes"
// @Success 200 {object} AlertRuleResponse "已更新规则 | Updated rule"
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.alert_rule.update"]
// @Router /alert-rules/{id} [patch]
// @Security BearerAuth
func (h *AlertRuleHandler) UpdateAlertRule(c *gin.Context) {
	id, tenantID, ok := alertRuleIdentity(c)
	if !ok {
		return
	}
	var request UpdateAlertRuleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidAlertRule)})
		return
	}
	rule, err := h.service.Update(c.Request.Context(), service.UpdateAlertRuleInput{
		TenantID: tenantID, ID: id, Name: request.Name, Module: request.Module, TaskType: request.TaskType,
		SourceTaskID: request.SourceTaskID, SourceTaskName: request.SourceTaskName, RuleType: request.RuleType,
		FailureThreshold: request.FailureThreshold, Severity: request.Severity, Enabled: request.Enabled, Routes: request.Routes,
	}, time.Now())
	if handleAlertRuleError(c, err) {
		return
	}
	c.JSON(http.StatusOK, rule)
}

// DeleteAlertRule 删除告警规则
// @Summary 删除告警规则 | Delete alert rule
// @Tags Monitor Alert Rules
// @Produce json
// @Param id path int true "规则 ID | Rule ID"
// @Success 200 {object} DeleteAlertRuleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.alert_rule.delete"]
// @Router /alert-rules/{id} [delete]
// @Security BearerAuth
func (h *AlertRuleHandler) DeleteAlertRule(c *gin.Context) {
	id, tenantID, ok := alertRuleIdentity(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), tenantID, id, time.Now()); handleAlertRuleError(c, err) {
		return
	}
	c.JSON(http.StatusOK, DeleteAlertRuleResponse{Message: commoni18n.T(c, moni18n.MsgAlertRuleDeleted)})
}

func alertRuleIdentity(c *gin.Context) (uint, int, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidAlertRuleID)})
		return 0, 0, false
	}
	tenantID, ok := alertTenantID(c)
	return uint(id), tenantID, ok
}

func handleAlertRuleError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, service.ErrAlertRuleInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidAlertRule)})
	case errors.Is(err, service.ErrAlertRuleNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgAlertRuleNotFound)})
	case errors.Is(err, service.ErrAlertRuleConflict):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, moni18n.MsgAlertRuleConflict)})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, moni18n.MsgAlertRuleOperationFailed)})
	}
	return true
}

package api

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type HTTPAlertRuleHandler struct {
	service *service.HTTPAlertRuleService
}
type HTTPAlertRuleResponse = models.HTTPAlertRule

type HTTPAlertRuleVersion struct {
	Version uint64 `json:"version" binding:"required"`
}

func httpRuleRespond(c *gin.Context, value any, err error) {
	if err != nil {
		if _, ok := client.SystemAPIStatusCode(err); ok || errors.Is(err, metricsdiscovery.ErrIdentityUnavailable) || errors.Is(err, context.DeadlineExceeded) {
			targetError(c, err)
			return
		}
	}
	platformOperationRespond(c, value, err, "http_rule")
}
func httpRuleRequest(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" {
		httpRuleRespond(c, nil, service.ErrLogInvalid)
		return false
	}
	return true
}

// List godoc
// @Summary 查询HTTP 接口质量告警规则 | List HTTP quality alert rules
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数 | Page size" default(20) maximum(100)
// @Success 200 {object} service.HTTPAlertRulePage
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_http_rule.read"]
// @Router /platform/http-alert-rules [get]
func (h *HTTPAlertRuleHandler) List(c *gin.Context) {
	query, parseErr := url.ParseQuery(c.Request.URL.RawQuery)
	if parseErr != nil {
		httpRuleRespond(c, nil, service.ErrLogInvalid)
		return
	}
	for k, v := range query {
		if (k != "page" && k != "page_size") || len(v) != 1 {
			httpRuleRespond(c, nil, service.ErrLogInvalid)
			return
		}
	}
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e != nil || e2 != nil || (c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0) {
		httpRuleRespond(c, nil, service.ErrLogInvalid)
		return
	}
	value, err := h.service.List(c.Request.Context(), page, size)
	httpRuleRespond(c, value, err)
}

// Create godoc
// @Summary 创建停用的HTTP 接口质量告警规则 | Create a disabled HTTP quality alert rule
// @Description 四项条件显式填写，新规则停用，实例仅用于当前用户的模块/角色核验；阈值按指标单位，0 < 恢复值 < 触发值，失败率上限100；时长15–300秒且为15的倍数 | Explicit thresholds and durations; rules start disabled, instance anchors current-user module/role authorization; thresholds use metric units, failure percentage at most 100, durations 15–300 seconds in multiples of 15
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth

// @Accept json
// @Param request body service.HTTPAlertRuleInput true "请求 | Request"
// @Success 201 {object} HTTPAlertRuleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_http_rule.read","monitor.platform_http_rule.create"]
// @Router /platform/http-alert-rules [post]
func (h *HTTPAlertRuleHandler) Create(c *gin.Context) {
	if !httpRuleRequest(c) {
		return
	}
	var input service.HTTPAlertRuleInput
	if !bindPlatformRequest(c, &input, httpRuleRespond) {
		return
	}
	value, err := h.service.Save(c.Request.Context(), "", targetToken(c), input, time.Now().UTC())
	if err == nil {
		c.JSON(201, value)
		return
	}
	httpRuleRespond(c, value, err)
}

// Update godoc
// @Summary 修改HTTP 接口质量告警规则 | Update HTTP quality alert rule
// @Description 模块、运行角色、模板和指标不可修改；活动告警沿用触发时保存的恢复条件 | Module, runtime role, template and metric are immutable; active alerts keep the recovery policy saved at opening
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则 ID | Rule ID"
// @Accept json
// @Param request body service.HTTPAlertRuleInput true "请求 | Request"
// @Success 200 {object} HTTPAlertRuleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_http_rule.read","monitor.platform_http_rule.update"]
// @Router /platform/http-alert-rules/{id} [put]
func (h *HTTPAlertRuleHandler) Update(c *gin.Context) {
	if !httpRuleRequest(c) {
		return
	}
	var input service.HTTPAlertRuleInput
	if !bindPlatformRequest(c, &input, httpRuleRespond) {
		return
	}
	value, err := h.service.Save(c.Request.Context(), c.Param("id"), targetToken(c), input, time.Now().UTC())
	httpRuleRespond(c, value, err)
}

// Delete godoc
// @Summary 删除HTTP 接口质量告警规则 | Delete HTTP quality alert rule
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则 ID | Rule ID"
// @Accept json
// @Param request body HTTPAlertRuleVersion true "请求 | Request"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_http_rule.read","monitor.platform_http_rule.delete"]
// @Router /platform/http-alert-rules/{id} [delete]
func (h *HTTPAlertRuleHandler) Delete(c *gin.Context) {
	if !httpRuleRequest(c) {
		return
	}
	var input HTTPAlertRuleVersion
	if !bindPlatformRequest(c, &input, httpRuleRespond) {
		return
	}
	err := h.service.Delete(c.Request.Context(), c.Param("id"), input.Version)
	httpRuleRespond(c, gin.H{}, err)
}

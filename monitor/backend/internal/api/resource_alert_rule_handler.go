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
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type ResourceAlertRuleHandler struct {
	service *service.ResourceAlertRuleService
}
type ResourceAlertRuleResponse = models.ResourceAlertRule

type ResourceAlertRuleVersion struct {
	Version uint64 `json:"version" binding:"required"`
}

func resourceRuleRespond(c *gin.Context, value any, err error) {
	if err != nil {
		if _, ok := client.SystemAPIStatusCode(err); ok || errors.Is(err, metricsdiscovery.ErrIdentityUnavailable) || errors.Is(err, context.DeadlineExceeded) {
			targetError(c, err)
			return
		}
	}
	platformOperationRespond(c, value, err, "resource_rule")
}
func ruleRequest(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" {
		resourceRuleRespond(c, nil, service.ErrLogInvalid)
		return false
	}
	return true
}

// List godoc
// @Summary 查询主机资源告警规则 | List host resource alert rules
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数 | Page size" default(20) maximum(100)
// @Param node_id query string false "主机 ID | Host ID"
// @Param metric_key query string false "固定指标键 | Fixed metric key" Enums(node.cpu.busy_percent,node.memory.used_percent,node.filesystem.used_percent)
// @Param device query string false "完整挂载设备，须同时提供主机、容量指标及另外两个维度 | Exact mount device; requires host, capacity metric and both other dimensions"
// @Param mountpoint query string false "完整挂载点 | Exact mountpoint"
// @Param fstype query string false "文件系统类型 | Exact filesystem type"
// @Success 200 {object} service.ResourceAlertRulePage
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_resource_rule.read"]
// @Router /platform/resource-alert-rules [get]
func (h *ResourceAlertRuleHandler) List(c *gin.Context) {
	query, parseErr := url.ParseQuery(c.Request.URL.RawQuery)
	if parseErr != nil {
		resourceRuleRespond(c, nil, service.ErrLogInvalid)
		return
	}
	filter := service.ResourceAlertRuleFilter{NodeID: query.Get("node_id"), MetricKey: query.Get("metric_key"), Dimensions: resourcequery.Dimensions{}}
	for k, v := range query {
		if (k != "page" && k != "page_size" && k != "node_id" && k != "metric_key" && k != "device" && k != "mountpoint" && k != "fstype") || len(v) != 1 || ((k == "metric_key" || k == "device" || k == "mountpoint" || k == "fstype") && v[0] == "") {
			resourceRuleRespond(c, nil, service.ErrLogInvalid)
			return
		}
		if k == "device" || k == "mountpoint" || k == "fstype" {
			filter.Dimensions[k] = v[0]
		}
	}
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e != nil || e2 != nil || (c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0) {
		resourceRuleRespond(c, nil, service.ErrLogInvalid)
		return
	}
	value, err := h.service.List(c.Request.Context(), page, size, filter)
	resourceRuleRespond(c, value, err)
}

// Create godoc
// @Summary 创建停用的主机资源告警规则 | Create a disabled host resource alert rule
// @Description 文件系统指标必须选择完整 device/mountpoint/fstype；标量指标 dimensions 为空。四项条件参数必须显式填写；0 < 恢复百分比 < 触发百分比 <= 100，持续时间为 15–300 秒且是 15 的倍数 | Filesystem metrics require exact device/mountpoint/fstype dimensions; scalar dimensions are empty. All four condition parameters are required; 0 < recovery percent < trigger percent <= 100; durations are 15–300 seconds in multiples of 15
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth

// @Accept json
// @Param request body service.ResourceAlertRuleInput true "请求 | Request"
// @Success 200 {object} ResourceAlertRuleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_resource_rule.read","monitor.platform_resource_rule.create"]
// @Router /platform/resource-alert-rules [post]
func (h *ResourceAlertRuleHandler) Create(c *gin.Context) {
	if !ruleRequest(c) {
		return
	}
	var input service.ResourceAlertRuleInput
	if !bindPlatformRequest(c, &input, resourceRuleRespond) {
		return
	}
	value, err := h.service.Save(c.Request.Context(), "", targetToken(c), input, time.Now().UTC())
	resourceRuleRespond(c, value, err)
}

// Update godoc
// @Summary 修改主机资源告警规则 | Update host resource alert rule
// @Description 主机、指标及挂载维度不可修改；活动告警沿用触发时保存的恢复条件 | Host, metric and mount dimensions are immutable; active alerts keep the recovery policy saved at opening
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则 ID | Rule ID"
// @Accept json
// @Param request body service.ResourceAlertRuleInput true "请求 | Request"
// @Success 200 {object} ResourceAlertRuleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_resource_rule.read","monitor.platform_resource_rule.update"]
// @Router /platform/resource-alert-rules/{id} [put]
func (h *ResourceAlertRuleHandler) Update(c *gin.Context) {
	if !ruleRequest(c) {
		return
	}
	var input service.ResourceAlertRuleInput
	if !bindPlatformRequest(c, &input, resourceRuleRespond) {
		return
	}
	value, err := h.service.Save(c.Request.Context(), c.Param("id"), targetToken(c), input, time.Now().UTC())
	resourceRuleRespond(c, value, err)
}

// Delete godoc
// @Summary 删除主机资源告警规则 | Delete host resource alert rule
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则 ID | Rule ID"
// @Accept json
// @Param request body ResourceAlertRuleVersion true "请求 | Request"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_resource_rule.read","monitor.platform_resource_rule.delete"]
// @Router /platform/resource-alert-rules/{id} [delete]
func (h *ResourceAlertRuleHandler) Delete(c *gin.Context) {
	if !ruleRequest(c) {
		return
	}
	var input ResourceAlertRuleVersion
	if !bindPlatformRequest(c, &input, resourceRuleRespond) {
		return
	}
	err := h.service.Delete(c.Request.Context(), c.Param("id"), input.Version)
	resourceRuleRespond(c, gin.H{}, err)
}

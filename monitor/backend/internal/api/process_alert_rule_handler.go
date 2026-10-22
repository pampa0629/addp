package api

import (
	"net/url"
	"strconv"
	"time"

	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type ProcessAlertRuleHandler struct {
	service *service.ProcessAlertRuleService
}
type ProcessAlertRuleResponse = models.ProcessAlertRule

type ProcessAlertRuleVersion struct {
	Version uint64 `json:"version" binding:"required"`
}

// List godoc
// @Summary 查询服务实例资源告警规则 | List service instance resource alert rules
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数 | Page size" default(20) maximum(100)
// @Success 200 {object} service.ProcessAlertRulePage
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_resource_rule.read"]
// @Router /platform/process-resource-alert-rules [get]
func (h *ProcessAlertRuleHandler) List(c *gin.Context) {
	query, parseErr := url.ParseQuery(c.Request.URL.RawQuery)
	if parseErr != nil {
		resourceRuleRespond(c, nil, service.ErrLogInvalid)
		return
	}
	for k, v := range query {
		if (k != "page" && k != "page_size") || len(v) != 1 {
			resourceRuleRespond(c, nil, service.ErrLogInvalid)
			return
		}
	}
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e != nil || e2 != nil || (c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0) {
		resourceRuleRespond(c, nil, service.ErrLogInvalid)
		return
	}
	value, err := h.service.List(c.Request.Context(), page, size)
	resourceRuleRespond(c, value, err)
}

// Create godoc
// @Summary 创建停用的服务实例资源告警规则 | Create a disabled service instance resource alert rule
// @Description 选择当前授权运行实例；CPU 阈值单位为核，RSS 为字节，恢复值须小于触发值。时长为15–300秒且是15的倍数，新建停用，重启不迁移 | Select a current authorized instance; CPU thresholds use cores and RSS uses bytes, with recovery below trigger. Durations are 15–300 seconds in steps of 15; new rules start disabled and do not migrate across restarts
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth

// @Accept json
// @Param request body service.ProcessAlertRuleInput true "请求 | Request"
// @Success 201 {object} ProcessAlertRuleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_resource_rule.read","monitor.platform_resource_rule.create"]
// @Router /platform/process-resource-alert-rules [post]
func (h *ProcessAlertRuleHandler) Create(c *gin.Context) {
	if !ruleRequest(c) {
		return
	}
	var input service.ProcessAlertRuleInput
	if !bindPlatformRequest(c, &input, resourceRuleRespond) {
		return
	}
	value, err := h.service.Save(c.Request.Context(), "", targetToken(c), input, time.Now().UTC())
	if err == nil {
		c.JSON(201, value)
		return
	}
	resourceRuleRespond(c, value, err)
}

// Update godoc
// @Summary 修改服务实例资源告警规则 | Update service instance resource alert rule
// @Description 实例身份、启动边界与指标不可修改；配置采用版本CAS，活动告警沿用打开时的恢复条件 | Instance identity, start boundary and metric are immutable; updates use version CAS and active alerts retain their opening recovery policy
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则 ID | Rule ID"
// @Accept json
// @Param request body service.ProcessAlertRuleInput true "请求 | Request"
// @Success 200 {object} ProcessAlertRuleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_resource_rule.read","monitor.platform_resource_rule.update"]
// @Router /platform/process-resource-alert-rules/{id} [put]
func (h *ProcessAlertRuleHandler) Update(c *gin.Context) {
	if !ruleRequest(c) {
		return
	}
	var input service.ProcessAlertRuleInput
	if !bindPlatformRequest(c, &input, resourceRuleRespond) {
		return
	}
	value, err := h.service.Save(c.Request.Context(), c.Param("id"), targetToken(c), input, time.Now().UTC())
	resourceRuleRespond(c, value, err)
}

// Delete godoc
// @Summary 删除服务实例资源告警规则 | Delete service instance resource alert rule
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则 ID | Rule ID"
// @Accept json
// @Param request body ProcessAlertRuleVersion true "请求 | Request"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_resource_rule.read","monitor.platform_resource_rule.delete"]
// @Router /platform/process-resource-alert-rules/{id} [delete]
func (h *ProcessAlertRuleHandler) Delete(c *gin.Context) {
	if !ruleRequest(c) {
		return
	}
	var input ProcessAlertRuleVersion
	if !bindPlatformRequest(c, &input, resourceRuleRespond) {
		return
	}
	err := h.service.Delete(c.Request.Context(), c.Param("id"), input.Version)
	resourceRuleRespond(c, gin.H{}, err)
}

package api

import (
	"net/url"
	"strconv"
	"time"

	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type ServiceAvailabilityRuleHandler struct {
	service *service.ServiceAvailabilityRuleService
}
type ServiceAvailabilityRuleResponse = models.ServiceAvailabilityRule

type ServiceAvailabilityRuleVersion struct {
	Version uint64 `json:"version" binding:"required"`
}

// List godoc
// @Summary 查询服务组失联告警规则 | List service group availability alert rules
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数 | Page size" default(20) maximum(100)
// @Success 200 {object} service.ServiceAvailabilityRulePage
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_service_availability_rule.read","platform.module.read"]
// @Router /platform/service-availability-alert-rules [get]
func (h *ServiceAvailabilityRuleHandler) List(c *gin.Context) {
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
	value, err := h.service.List(c.Request.Context(), targetToken(c), page, size)
	resourceRuleRespond(c, value, err)
}

// Create godoc
// @Summary 创建停用的服务组失联告警规则 | Create a disabled service group availability alert rule
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth

// @Accept json
// @Param request body service.ServiceAvailabilityRuleInput true "请求 | Request"
// @Success 201 {object} ServiceAvailabilityRuleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_service_availability_rule.read","monitor.platform_service_availability_rule.create","platform.module.read"]
// @Router /platform/service-availability-alert-rules [post]
func (h *ServiceAvailabilityRuleHandler) Create(c *gin.Context) {
	if !ruleRequest(c) {
		return
	}
	var input service.ServiceAvailabilityRuleInput
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
// @Summary 修改服务组失联告警规则 | Update service group availability alert rule
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则 ID | Rule ID"
// @Accept json
// @Param request body service.ServiceAvailabilityRuleInput true "请求 | Request"
// @Success 200 {object} ServiceAvailabilityRuleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_service_availability_rule.read","monitor.platform_service_availability_rule.update","platform.module.read"]
// @Router /platform/service-availability-alert-rules/{id} [put]
func (h *ServiceAvailabilityRuleHandler) Update(c *gin.Context) {
	if !ruleRequest(c) {
		return
	}
	var input service.ServiceAvailabilityRuleInput
	if !bindPlatformRequest(c, &input, resourceRuleRespond) {
		return
	}
	value, err := h.service.Save(c.Request.Context(), c.Param("id"), targetToken(c), input, time.Now().UTC())
	resourceRuleRespond(c, value, err)
}

// Delete godoc
// @Summary 删除服务组失联告警规则 | Delete service group availability alert rule
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则 ID | Rule ID"
// @Accept json
// @Param request body ServiceAvailabilityRuleVersion true "请求 | Request"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_service_availability_rule.read","monitor.platform_service_availability_rule.delete","platform.module.read"]
// @Router /platform/service-availability-alert-rules/{id} [delete]
func (h *ServiceAvailabilityRuleHandler) Delete(c *gin.Context) {
	if !ruleRequest(c) {
		return
	}
	var input ServiceAvailabilityRuleVersion
	if !bindPlatformRequest(c, &input, resourceRuleRespond) {
		return
	}
	err := h.service.Delete(c.Request.Context(), targetToken(c), c.Param("id"), input.Version)
	resourceRuleRespond(c, gin.H{}, err)
}

// Groups godoc
// @Summary 查询当前授权服务组 | List current authorized service groups
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.ServiceAvailabilityGroupPage
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_service_availability_rule.read","platform.module.read"]
// @Router /platform/service-availability-alert-rules/groups [get]
func (h *ServiceAvailabilityRuleHandler) Groups(c *gin.Context) {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		resourceRuleRespond(c, nil, service.ErrLogInvalid)
		return
	}
	value, err := h.service.Groups(c.Request.Context(), targetToken(c))
	resourceRuleRespond(c, value, err)
}

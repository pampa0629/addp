package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	auth "github.com/addp/common/middleware/auth"
	i18n "github.com/addp/common/middleware/i18n"
	monitori18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type MonitoringTargetHandler struct {
	targets *service.MonitoringTargetService
}

func NewMonitoringTargetHandler(targets *service.MonitoringTargetService) *MonitoringTargetHandler {
	return &MonitoringTargetHandler{targets: targets}
}

type MonitoringTargetVersion struct {
	Version int64 `json:"version" binding:"required,min=1"`
}

func platformObservationIdentity(serviceToken bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		identity, ok := auth.AuthContextFromGin(c)
		valid := ok && identity.Context.Type == "platform" && identity.Delegation == nil
		if serviceToken {
			valid = valid && identity.Principal.Type == "service_principal" && identity.Token.Type == "service_access_token" && auth.GetClientID(c) == "addp-prometheus"
		} else {
			valid = valid && identity.Principal.Type == "user" && (identity.Token.Type == "first_party_access_token" || identity.Token.Type == "oauth_access_token")
		}
		if !valid {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": i18n.T(c, i18n.MsgForbidden), "error_code": "permission_denied"})
			return
		}
		c.Next()
	}
}
func targetToken(c *gin.Context) string {
	return auth.CanonicalBearerToken(c.GetHeader("Authorization"))
}
func targetID(c *gin.Context) bool {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil || id.String() != c.Param("id") {
		targetError(c, metricsdiscovery.ErrInvalidTarget)
		return false
	}
	return true
}
func targetNoQuery(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" {
		targetError(c, metricsdiscovery.ErrInvalidTarget)
		return false
	}
	return true
}
func targetNoBody(c *gin.Context) bool {
	if c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		targetError(c, metricsdiscovery.ErrInvalidTarget)
		return false
	}
	return true
}
func targetBind(c *gin.Context, input any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := commonapi.BindOptionalJSONStrict(c, input); err != nil {
		targetError(c, metricsdiscovery.ErrInvalidTarget)
		return false
	}
	return true
}
func targetError(c *gin.Context, err error) {
	status, code, key := http.StatusServiceUnavailable, "obs_unavailable", monitori18n.MsgMetricsUnavailable
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		status, code, key = 504, "obs_timeout", monitori18n.MsgMetricsTimeout
	case errors.Is(err, metricsdiscovery.ErrDisabled):
		status, code, key = 409, "obs_capability_disabled", monitori18n.MsgMetricsDisabled
	case errors.Is(err, metricsdiscovery.ErrUnconfigured):
		status, code, key = 503, "obs_unconfigured", monitori18n.MsgMetricsUnconfigured
	case errors.Is(err, metricsdiscovery.ErrBudgetExceeded):
		status, code, key = 422, "obs_budget_exceeded", monitori18n.MsgMetricsBudget
	case errors.Is(err, repository.ErrTargetConflict):
		status, code, key = 409, "resource_version_conflict", monitori18n.MsgMetricsConflict
	case errors.Is(err, repository.ErrTargetNotFound):
		status, code, key = 404, "resource_not_found", monitori18n.MsgMetricsNotFound
	case errors.Is(err, repository.ErrTargetBusy):
		status, code, key = 429, "obs_concurrency_exceeded", monitori18n.MsgMetricsBusy
	case errors.Is(err, metricsdiscovery.ErrInvalidTarget), errors.Is(err, metricsdiscovery.ErrEndpointForbidden):
		status, code, key = 400, "invalid_monitoring_target", monitori18n.MsgMetricsInvalid
	case errors.Is(err, metricsdiscovery.ErrUnprotected), errors.Is(err, metricsdiscovery.ErrAdmissionFailed):
		status, code, key = 422, "obs_source_admission_failed", monitori18n.MsgMetricsAdmission
	default:
		if ownerStatus, ok := client.SystemAPIStatusCode(err); ok {
			switch ownerStatus {
			case 401:
				status, code, key = 401, "authentication_required", i18n.MsgUnauthorized
			case 403:
				status, code, key = 403, "permission_denied", i18n.MsgForbidden
			case 404:
				status, code, key = 404, "resource_not_found", monitori18n.MsgMetricsNotFound
			}
		}
	}
	c.JSON(status, gin.H{"error": i18n.T(c, key), "error_code": code})
}

// List godoc
// @Summary 查询监测目标 | List monitoring targets
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数 | Page size" default(20) maximum(100)
// @Success 200 {object} service.MonitoringTargetPage
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.monitoring_target.read"]
// @Router /platform/monitoring_targets [get]
func (h *MonitoringTargetHandler) List(c *gin.Context) {

	if !targetNoBody(c) {
		return
	}
	page, size := 1, 20
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		targetError(c, metricsdiscovery.ErrInvalidTarget)
		return
	}
	for key, values := range query {
		if len(values) != 1 || (key != "page" && key != "page_size") {
			targetError(c, metricsdiscovery.ErrInvalidTarget)
			return
		}
		n, err := strconv.Atoi(values[0])
		if err != nil || strconv.Itoa(n) != values[0] || n < 1 || (key == "page" && n > 1000000) || (key == "page_size" && n > 100) {
			targetError(c, metricsdiscovery.ErrInvalidTarget)
			return
		}
		if key == "page" {
			page = n
		} else {
			size = n
		}
	}
	result, err := h.targets.List(c.Request.Context(), targetToken(c), page, size)
	if err != nil {
		targetError(c, err)
		return
	}
	c.JSON(200, result)
}

// Get godoc
// @Summary 获取监测目标 | Get monitoring target
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param id path string true "目标 UUID | Target UUID"
// @Success 200 {object} metricsdiscovery.NodeTarget
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.monitoring_target.read"]
// @Router /platform/monitoring_targets/{id} [get]
func (h *MonitoringTargetHandler) Get(c *gin.Context) {
	if !targetNoQuery(c) {
		return
	}
	if !targetID(c) {
		return
	}
	if !targetNoBody(c) {
		return
	}
	result, err := h.targets.Get(c.Request.Context(), c.Param("id"), targetToken(c))
	if err != nil {
		targetError(c, err)
		return
	}
	c.JSON(200, result)
}

// Create godoc
// @Summary 创建监测目标 | Create monitoring target
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Accept json
// @Param input body service.MonitoringTargetInput true "目标配置与版本 | Target configuration and version"
// @Success 201 {object} metricsdiscovery.NodeTarget
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.monitoring_target.create"]
// @Router /platform/monitoring_targets [post]
func (h *MonitoringTargetHandler) Create(c *gin.Context) {
	if !targetNoQuery(c) {
		return
	}
	var input service.MonitoringTargetInput
	if !targetBind(c, &input) {
		return
	}
	result, err := h.targets.Save(c.Request.Context(), "", targetToken(c), input)
	if err != nil {
		targetError(c, err)
		return
	}
	c.JSON(201, result)
}

// Update godoc
// @Summary 更新监测目标 | Update monitoring target
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param id path string true "目标 UUID | Target UUID"
// @Accept json
// @Param input body service.MonitoringTargetInput true "目标配置与版本 | Target configuration and version"
// @Success 200 {object} metricsdiscovery.NodeTarget
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.monitoring_target.update"]
// @Router /platform/monitoring_targets/{id} [put]
func (h *MonitoringTargetHandler) Update(c *gin.Context) {
	if !targetNoQuery(c) {
		return
	}
	if !targetID(c) {
		return
	}
	var input service.MonitoringTargetInput
	if !targetBind(c, &input) {
		return
	}
	result, err := h.targets.Save(c.Request.Context(), c.Param("id"), targetToken(c), input)
	if err != nil {
		targetError(c, err)
		return
	}
	c.JSON(200, result)
}

// Delete godoc
// @Summary 删除监测目标 | Delete monitoring target
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param id path string true "目标 UUID | Target UUID"
// @Accept json
// @Param input body MonitoringTargetVersion true "目标配置与版本 | Target configuration and version"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.monitoring_target.delete"]
// @Router /platform/monitoring_targets/{id} [delete]
func (h *MonitoringTargetHandler) Delete(c *gin.Context) {
	if !targetNoQuery(c) {
		return
	}
	if !targetID(c) {
		return
	}
	var input MonitoringTargetVersion
	if !targetBind(c, &input) {
		return
	}
	if err := h.targets.Delete(c.Request.Context(), c.Param("id"), targetToken(c), input.Version); err != nil {
		targetError(c, err)
		return
	}
	c.Status(204)
}

// Discovery godoc
// @Summary 获取受认证指标发现 | Get authenticated metrics discovery
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Success 200 {array} metricsdiscovery.TargetGroup
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.metrics_discovery.read"]
// @Router /platform/metrics_discovery [get]
func (h *MonitoringTargetHandler) Discovery(c *gin.Context) {
	if !targetNoQuery(c) {
		return
	}
	if !targetNoBody(c) {
		return
	}
	result, err := h.targets.Discover(c.Request.Context())
	if err != nil {
		targetError(c, err)
		return
	}
	c.JSON(200, result)
}

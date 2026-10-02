package api

import (
	"encoding/json"
	"errors"
	"github.com/addp/common/logpipeline"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	monitori18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strconv"
	"time"
)

type LogPipelineHandler struct {
	pipeline      *service.LogPipelineService
	notifications *service.PlatformLogNotifications
}

func NewLogPipelineHandler(p *service.LogPipelineService, n *service.PlatformLogNotifications) *LogPipelineHandler {
	return &LogPipelineHandler{pipeline: p, notifications: n}
}

type LogIncidentAction struct {
	Version         uint64     `json:"version" binding:"required"`
	SuppressedUntil *time.Time `json:"suppressed_until,omitempty"`
}
type LogCredentialInput struct {
	Version uint64 `json:"version" binding:"required"`
	Secret  string `json:"secret" binding:"required"`
}

// Summary godoc
// @Summary 获取平台日志链路状态 | Get platform log pipeline state
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.LogPipelineSummary
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_pipeline.read"]
// @Router /platform/log-pipeline [get]
func (h *LogPipelineHandler) Summary(c *gin.Context) {
	result, err := h.pipeline.Summary(c.Request.Context(), time.Now().UTC())
	logRespond(c, result, err)
}

// Policy godoc
// @Summary 获取平台日志链路策略 | Get platform log pipeline policy
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.LogPipelinePolicy
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_pipeline.read"]
// @Router /platform/log-pipeline/policy [get]
func (h *LogPipelineHandler) Policy(c *gin.Context) {
	result, err := h.pipeline.Policy(c.Request.Context())
	logRespond(c, result, err)
}

// UpdatePolicy godoc
// @Summary 更新平台日志链路策略 | Update platform log pipeline policy
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Accept json
// @Param request body models.LogPipelinePolicy true "请求 | Request"
// @Success 200 {object} models.LogPipelinePolicy
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_pipeline.update"]
// @Router /platform/log-pipeline/policy [put]
func (h *LogPipelineHandler) UpdatePolicy(c *gin.Context) {
	var input models.LogPipelinePolicy
	if !logBind(c, &input) {
		return
	}
	result, err := h.pipeline.UpdatePolicy(c.Request.Context(), input)
	logRespond(c, result, err)
}

// Observe godoc
// @Summary 上报平台日志链路观测 | Report platform log observation
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Accept json
// @Param request body logpipeline.Observation true "请求 | Request"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_observation.create"]
// @Router /platform/log-observations [post]
func (h *LogPipelineHandler) Observe(c *gin.Context) {
	identity, ok := commonAuth.AuthContextFromGin(c)
	if !ok || identity.Token.Type != "service_access_token" || identity.Delegation != nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": "permission_denied"})
		return
	}
	var input logpipeline.Observation
	if !logBind(c, &input) {
		return
	}
	err := h.pipeline.Ingest(c.Request.Context(), input, time.Now().UTC())
	logRespond(c, gin.H{"accepted": err == nil}, err)
}

// Acknowledge godoc
// @Summary 确认平台日志链路告警 | Acknowledge platform log incident
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body LogIncidentAction true "请求 | Request"
// @Success 200 {object} models.PlatformLogIncident
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_pipeline.update"]
// @Router /platform/log-pipeline/incidents/{id}/acknowledge [post]
func (h *LogPipelineHandler) Acknowledge(c *gin.Context) { h.manageIncident(c, "acknowledge") }

// Suppress godoc
// @Summary 限时抑制平台日志告警通知 | Suppress platform log notifications
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body LogIncidentAction true "请求 | Request"
// @Success 200 {object} models.PlatformLogIncident
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_pipeline.update"]
// @Router /platform/log-pipeline/incidents/{id}/suppress [post]
func (h *LogPipelineHandler) Suppress(c *gin.Context) { h.manageIncident(c, "suppress") }

// Destinations godoc
// @Summary 查询平台日志通知目标 | List platform log notification destinations
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Success 200 {array} models.PlatformLogDestination
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_notification.read"]
// @Router /platform/log-notification-destinations [get]
func (h *LogPipelineHandler) Destinations(c *gin.Context) {
	result, err := h.notifications.List(c.Request.Context())
	logRespond(c, result, err)
}

// CreateDestination godoc
// @Summary 创建平台日志通知目标 | Create platform log notification destination
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Accept json
// @Param request body service.LogDestinationInput true "请求 | Request"
// @Success 200 {object} models.PlatformLogDestination
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_notification.update"]
// @Router /platform/log-notification-destinations [post]
func (h *LogPipelineHandler) CreateDestination(c *gin.Context) {
	var input service.LogDestinationInput
	if !logBind(c, &input) {
		return
	}
	result, err := h.notifications.Save(c.Request.Context(), 0, input)
	logRespond(c, result, err)
}

// UpdateDestination godoc
// @Summary 更新平台日志通知目标 | Update platform log notification destination
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body service.LogDestinationInput true "请求 | Request"
// @Success 200 {object} models.PlatformLogDestination
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_notification.update"]
// @Router /platform/log-notification-destinations/{id} [put]
func (h *LogPipelineHandler) UpdateDestination(c *gin.Context) {
	id, ok := logID(c)
	if !ok {
		return
	}
	var input service.LogDestinationInput
	if !logBind(c, &input) {
		return
	}
	result, err := h.notifications.Save(c.Request.Context(), id, input)
	logRespond(c, result, err)
}

// SetCredential godoc
// @Summary 设置平台日志通知签名凭据 | Set platform log notification signing credential
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body LogCredentialInput true "请求 | Request"
// @Success 200 {object} models.PlatformLogDestination
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_notification.update"]
// @Router /platform/log-notification-destinations/{id}/credential [put]
func (h *LogPipelineHandler) SetCredential(c *gin.Context) {
	id, ok := logID(c)
	if !ok {
		return
	}
	var input LogCredentialInput
	if !logBind(c, &input) {
		return
	}
	result, err := h.notifications.SetSecret(c.Request.Context(), id, input.Version, input.Secret)
	logRespond(c, result, err)
}

// DeleteDestination godoc
// @Summary 删除平台日志通知目标 | Delete platform log notification destination
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body LogIncidentAction true "请求 | Request"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_notification.update"]
// @Router /platform/log-notification-destinations/{id} [delete]
func (h *LogPipelineHandler) DeleteDestination(c *gin.Context) {
	id, ok := logID(c)
	if !ok {
		return
	}
	var input LogIncidentAction
	if !logBind(c, &input) {
		return
	}
	err := h.notifications.Delete(c.Request.Context(), id, input.Version)
	logRespond(c, gin.H{"deleted": err == nil}, err)
}

// TestDestination godoc
// @Summary 测试平台日志通知目标 | Test platform log notification destination
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_notification.update"]
// @Router /platform/log-notification-destinations/{id}/test [post]
func (h *LogPipelineHandler) TestDestination(c *gin.Context) {
	id, ok := logID(c)
	if !ok {
		return
	}
	err := h.notifications.Test(c.Request.Context(), id, time.Now().UTC())
	logRespond(c, gin.H{"sent": err == nil}, err)
}

// Deliveries godoc
// @Summary 查询平台日志通知投递记录 | List platform log notification deliveries
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数 | Page size" default(20)
// @Success 200 {object} service.LogDeliveryList
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_notification.read"]
// @Router /platform/log-notification-deliveries [get]
func (h *LogPipelineHandler) Deliveries(c *gin.Context) {
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e != nil || e2 != nil {
		logRespond(c, nil, service.ErrLogInvalid)
		return
	}
	result, err := h.notifications.Deliveries(c.Request.Context(), page, size)
	logRespond(c, result, err)
}

// RetryDelivery godoc
// @Summary 重新入队最终失败的平台日志通知 | Requeue a failed platform log notification
// @Description 保留原投递及事件身份，允许补发已恢复告警的历史消息；200 仅表示重新入队。 | Retains delivery/event identities and permits historical messages for resolved incidents; 200 means requeued, not delivered.
// @Tags 平台日志链路 | Platform Log Pipeline
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "投递 UUID | Delivery UUID"
// @Param request body service.LogDeliveryRetryInput true "预期重投次数和目标版本 | Expected retry count and destination version"
// @Success 200 {object} service.LogDeliveryView
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_notification.update"]
// @Router /platform/log-notification-deliveries/{id}/retry [post]
func (h *LogPipelineHandler) RetryDelivery(c *gin.Context) {
	identity, ok := commonAuth.AuthContextFromGin(c)
	if !ok || identity.Principal.Type != "user" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": "permission_denied"})
		return
	}
	id := c.Param("id")
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		logRespond(c, nil, service.ErrLogInvalid)
		return
	}
	var input service.LogDeliveryRetryInput
	if !logBind(c, &input) {
		return
	}
	result, err := h.notifications.Retry(c.Request.Context(), id, input, time.Now().UTC())
	if err == nil {
		c.Set(logRetryAuditKey, logRetryAuditFacts{DeliveryID: id, EventID: result.EventID, DestinationID: result.DestinationID, Before: result.ManualRetryCount - 1, After: result.ManualRetryCount})
	}
	if errors.Is(err, service.ErrLogConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, monitori18n.MsgLogRetryConflict), "error_code": "resource_version_conflict"})
		return
	}
	logRespond(c, result, err)
}

func (h *LogPipelineHandler) manageIncident(c *gin.Context, action string) {
	id, ok := logID(c)
	if !ok {
		return
	}
	var input LogIncidentAction
	if !logBind(c, &input) {
		return
	}
	principal, ok := commonAuth.PrincipalIDFromGin(c)
	if !ok {
		c.AbortWithStatus(401)
		return
	}
	result, err := h.pipeline.ManageIncident(c.Request.Context(), id, input.Version, action, input.SuppressedUntil, principal, time.Now().UTC())
	logRespond(c, result, err)
}
func logID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		logRespond(c, nil, service.ErrLogInvalid)
		return 0, false
	}
	return uint(id), true
}
func logBind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(v) != nil {
		logRespond(c, nil, service.ErrLogInvalid)
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		logRespond(c, nil, service.ErrLogInvalid)
		return false
	}
	return true
}
func logRespond(c *gin.Context, v any, err error) {
	if err == nil {
		c.JSON(200, v)
		return
	}
	status := 500
	key := monitori18n.MsgDiagnosticQueryFailed
	code := "platform_log_operation_failed"
	switch {
	case errors.Is(err, service.ErrLogRetryUnavailable):
		status = 409
		key = monitori18n.MsgLogRetryUnavailable
		code = "platform_log_retry_unavailable"
	case errors.Is(err, service.ErrLogInvalid):
		status = 400
		key = monitori18n.MsgConfigurationInvalid
		code = "platform_log_invalid"
	case errors.Is(err, service.ErrLogConflict):
		status = 409
		key = monitori18n.MsgConfigurationConflict
		code = "resource_version_conflict"
	case errors.Is(err, service.ErrLogNotFound):
		status = 404
		key = monitori18n.MsgInvalidAlertID
		code = "platform_log_not_found"
	case errors.Is(err, service.ErrEmailSenderUnavailable):
		status = 503
		key = monitori18n.MsgEmailSenderUnavailable
		code = "smtp_unconfigured"
	}
	c.JSON(status, gin.H{"error": commoni18n.T(c, key), "error_code": code})
}

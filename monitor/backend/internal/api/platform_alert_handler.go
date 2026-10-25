package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	monitori18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PlatformAlertHandler struct {
	service *service.PlatformAlertService
}

func NewPlatformAlertHandler(s *service.PlatformAlertService) *PlatformAlertHandler {
	return &PlatformAlertHandler{service: s}
}

type PlatformAlertAction struct {
	Version         uint64     `json:"version" binding:"required"`
	SuppressedUntil *time.Time `json:"suppressed_until,omitempty"`
}
type PlatformAlertCredentialInput struct {
	Version uint64 `json:"version" binding:"required"`
	Secret  string `json:"secret" binding:"required"` // Webhook HMAC 密钥；企业微信完整机器人地址 | Webhook HMAC secret; WeCom full robot URL.
}

func platformAlertID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		platformAlertRespond(c, nil, service.ErrLogInvalid)
		return 0, false
	}
	return uint(id), true
}

// Incidents godoc
// @Summary 查询平台告警 | List platform alerts
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param category query string false "类别 | Category" Enums(log_pipeline,resource,http_quality,process_resource)
// @Param status query string false "状态 | Status" Enums(active,open,acknowledged,resolved)
// @Param search query string false "触发时告警名称，字面包含、忽略大小写，最多100字符 | Opening alert name, case-insensitive literal substring, at most 100 characters"
// @Param host_name query string false "System 当前主机名称，字面包含、忽略大小写、最多100字符；仅主机资源告警，额外需要主机读取权 | Current System host name, case-insensitive literal substring, at most 100 characters; host resource alerts only, additionally requires host read permission"
// @Param severity query string false "级别（省略为全部） | Severity (omitted for all)" Enums(critical,warning)
// @Param time_from query string false "发现时间下界，RFC3339，含 | Inclusive opening time lower bound, RFC3339"
// @Param time_to query string false "发现时间上界，RFC3339，不含 | Exclusive opening time upper bound, RFC3339"
// @Param subject_id query string false "主体 ID | Subject ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数 | Page size" default(20)
// @Success 200 {object} service.PlatformAlertList
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert.read"]
// @x-addp-conditional-permissions ["platform.host_node.read"]
// @Router /platform/alerts [get]
func (h *PlatformAlertHandler) Incidents(c *gin.Context) {
	query, parseErr := url.ParseQuery(c.Request.URL.RawQuery)
	if parseErr != nil {
		platformAlertRespond(c, nil, service.ErrLogInvalid)
		return
	}
	for key, values := range query {
		switch key {
		case "page", "page_size", "category", "status", "subject_id", "severity", "time_from", "time_to", "search", "host_name":
		default:
			platformAlertRespond(c, nil, service.ErrLogInvalid)
			return
		}
		if len(values) != 1 {
			platformAlertRespond(c, nil, service.ErrLogInvalid)
			return
		}
	}
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e != nil || e2 != nil {
		platformAlertRespond(c, nil, service.ErrLogInvalid)
		return
	}
	filter := service.PlatformAlertFilter{Page: page, PageSize: size, Category: query.Get("category"), Status: query.Get("status"), SubjectID: query.Get("subject_id"), Severity: query.Get("severity"), Search: query.Get("search"), HostName: query.Get("host_name"), Language: commoni18n.GetLang(c)}
	for _, bound := range []struct {
		raw    string
		target **time.Time
	}{{query.Get("time_from"), &filter.TimeFrom}, {query.Get("time_to"), &filter.TimeTo}} {
		if bound.raw == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, bound.raw)
		if err != nil {
			platformAlertRespond(c, nil, service.ErrLogInvalid)
			return
		}
		*bound.target = &parsed
	}
	if strings.TrimSpace(filter.HostName) != "" {
		commonAuth.MustNewPermissionGuard("platform.host_node.read")(c)
		if c.IsAborted() {
			return
		}
	}
	result, err := h.service.Incidents(c.Request.Context(), filter, targetToken(c))
	platformAlertRespond(c, result, err)
}

// Source capability and transport errors keep their shared codes; management
// operation errors belong to the platform alert domain.
func platformAlertRespond(c *gin.Context, v any, err error) {
	if errors.Is(err, metricsdiscovery.ErrIdentityUnavailable) || errors.Is(err, resourcequery.ErrBudget) || errors.Is(err, context.DeadlineExceeded) {
		resourceError(c, err)
		return
	}
	platformOperationRespond(c, v, err, "platform_alert")
}

// Acknowledge godoc
// @Summary 确认平台告警 | Acknowledge platform alert incident
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body PlatformAlertAction true "请求 | Request"
// @Success 200 {object} models.PlatformAlertIncident
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert.read","monitor.platform_alert.update"]
// @Router /platform/alerts/{id}/acknowledge [post]
func (h *PlatformAlertHandler) Acknowledge(c *gin.Context) { h.manageIncident(c, "acknowledge") }

// Suppress godoc
// @Summary 限时抑制平台告警通知 | Suppress platform alert notifications
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body PlatformAlertAction true "请求 | Request"
// @Success 200 {object} models.PlatformAlertIncident
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert.read","monitor.platform_alert.update"]
// @Router /platform/alerts/{id}/suppress [post]
func (h *PlatformAlertHandler) Suppress(c *gin.Context) { h.manageIncident(c, "suppress") }

// Destinations godoc
// @Summary 查询平台告警通知目标 | List platform alert notification destinations
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Success 200 {array} models.PlatformAlertDestination
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert_notification.read"]
// @Router /platform/alert-notification-destinations [get]
func (h *PlatformAlertHandler) Destinations(c *gin.Context) {
	result, err := h.service.Destinations(c.Request.Context())
	platformAlertRespond(c, result, err)
}

// CreateDestination godoc
// @Summary 创建平台告警通知目标 | Create platform alert notification destination
// @Description 渠道为 webhook/email/wecom；订阅仅支持 opened/resolved；wecom 的 url 与 recipients 留空，凭据另行设置 | Channels are webhook/email/wecom; subscriptions only support opened/resolved; wecom requires empty url and recipients with a separate credential write
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Accept json
// @Param request body service.PlatformAlertDestinationInput true "请求 | Request"
// @Success 200 {object} models.PlatformAlertDestination
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert_notification.read","monitor.platform_alert_notification.update"]
// @Router /platform/alert-notification-destinations [post]
func (h *PlatformAlertHandler) CreateDestination(c *gin.Context) {
	var input service.PlatformAlertDestinationInput
	if !bindPlatformRequest(c, &input, platformAlertRespond) {
		return
	}
	result, err := h.service.Save(c.Request.Context(), 0, input)
	platformAlertRespond(c, result, err)
}

// UpdateDestination godoc
// @Description 订阅类别必须显式选择；事件仅支持 opened/resolved | Categories must be selected explicitly; events only support opened/resolved
// @Summary 更新平台告警通知目标 | Update platform alert notification destination
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body service.PlatformAlertDestinationInput true "请求 | Request"
// @Success 200 {object} models.PlatformAlertDestination
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert_notification.read","monitor.platform_alert_notification.update"]
// @Router /platform/alert-notification-destinations/{id} [put]
func (h *PlatformAlertHandler) UpdateDestination(c *gin.Context) {
	id, ok := platformAlertID(c)
	if !ok {
		return
	}
	var input service.PlatformAlertDestinationInput
	if !bindPlatformRequest(c, &input, platformAlertRespond) {
		return
	}
	result, err := h.service.Save(c.Request.Context(), id, input)
	platformAlertRespond(c, result, err)
}

// SetCredential godoc
// @Summary 设置平台告警通知渠道凭据 | Set platform alert notification channel credential
// @Description Webhook 使用 HMAC 密钥；企业微信使用完整机器人地址，key 加密保存且不回显 | Webhook uses an HMAC secret; WeCom uses the full robot URL, with its key encrypted and never returned
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body PlatformAlertCredentialInput true "请求 | Request"
// @Success 200 {object} models.PlatformAlertDestination
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert_notification.read","monitor.platform_alert_notification.update"]
// @Router /platform/alert-notification-destinations/{id}/credential [put]
func (h *PlatformAlertHandler) SetCredential(c *gin.Context) {
	id, ok := platformAlertID(c)
	if !ok {
		return
	}
	var input PlatformAlertCredentialInput
	if !bindPlatformRequest(c, &input, platformAlertRespond) {
		return
	}
	result, err := h.service.SetSecret(c.Request.Context(), id, input.Version, input.Secret)
	platformAlertRespond(c, result, err)
}

// DeleteDestination godoc
// @Summary 删除平台告警通知目标 | Delete platform alert notification destination
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Accept json
// @Param request body PlatformAlertAction true "请求 | Request"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert_notification.read","monitor.platform_alert_notification.update"]
// @Router /platform/alert-notification-destinations/{id} [delete]
func (h *PlatformAlertHandler) DeleteDestination(c *gin.Context) {
	id, ok := platformAlertID(c)
	if !ok {
		return
	}
	var input PlatformAlertAction
	if !bindPlatformRequest(c, &input, platformAlertRespond) {
		return
	}
	err := h.service.Delete(c.Request.Context(), id, input.Version)
	platformAlertRespond(c, gin.H{"deleted": err == nil}, err)
}

// TestDestination godoc
// @Summary 测试平台告警通知目标 | Test platform alert notification destination
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param id path int true "资源 ID | Resource ID"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert_notification.read","monitor.platform_alert_notification.update"]
// @Router /platform/alert-notification-destinations/{id}/test [post]
func (h *PlatformAlertHandler) TestDestination(c *gin.Context) {
	id, ok := platformAlertID(c)
	if !ok {
		return
	}
	err := h.service.Test(c.Request.Context(), id, time.Now().UTC())
	platformAlertRespond(c, gin.H{"sent": err == nil}, err)
}

// Deliveries godoc
// @Summary 查询平台告警通知投递记录 | List platform alert notification deliveries
// @Tags 平台告警 | Platform Alerts
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数 | Page size" default(20)
// @Success 200 {object} service.PlatformAlertDeliveryList
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert_notification.read"]
// @Router /platform/alert-notification-deliveries [get]
func (h *PlatformAlertHandler) Deliveries(c *gin.Context) {
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e != nil || e2 != nil {
		platformAlertRespond(c, nil, service.ErrLogInvalid)
		return
	}
	result, err := h.service.Deliveries(c.Request.Context(), page, size)
	platformAlertRespond(c, result, err)
}

// RetryDelivery godoc
// @Summary 重新入队最终失败的平台告警通知 | Requeue a failed platform alert notification
// @Description 保留原投递及事件身份，允许补发已恢复告警的历史消息；200 仅表示重新入队。 | Retains delivery/event identities and permits historical messages for resolved incidents; 200 means requeued, not delivered.
// @Tags 平台告警 | Platform Alerts
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "投递 UUID | Delivery UUID"
// @Param request body service.PlatformAlertDeliveryRetryInput true "预期重投次数和目标版本 | Expected retry count and destination version"
// @Success 200 {object} service.PlatformAlertDeliveryView
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.platform_alert_notification.read","monitor.platform_alert_notification.update"]
// @Router /platform/alert-notification-deliveries/{id}/retry [post]
func (h *PlatformAlertHandler) RetryDelivery(c *gin.Context) {
	identity, ok := commonAuth.AuthContextFromGin(c)
	if !ok || identity.Principal.Type != "user" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": "permission_denied"})
		return
	}
	id := c.Param("id")
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		platformAlertRespond(c, nil, service.ErrLogInvalid)
		return
	}
	var input service.PlatformAlertDeliveryRetryInput
	if !bindPlatformRequest(c, &input, platformAlertRespond) {
		return
	}
	result, err := h.service.Retry(c.Request.Context(), id, input, time.Now().UTC())
	if err == nil {
		c.Set(logRetryAuditKey, logRetryAuditFacts{DeliveryID: id, EventID: result.EventID, DestinationID: result.DestinationID, Before: result.ManualRetryCount - 1, After: result.ManualRetryCount})
	}
	if errors.Is(err, service.ErrLogConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, monitori18n.MsgLogRetryConflict), "error_code": "resource_version_conflict"})
		return
	}
	platformAlertRespond(c, result, err)
}

func (h *PlatformAlertHandler) manageIncident(c *gin.Context, action string) {
	id, ok := platformAlertID(c)
	if !ok {
		return
	}
	var input PlatformAlertAction
	if !bindPlatformRequest(c, &input, platformAlertRespond) {
		return
	}
	principal, ok := commonAuth.PrincipalIDFromGin(c)
	if !ok {
		c.AbortWithStatus(401)
		return
	}
	var result models.PlatformAlertIncident
	result, err := h.service.ManageIncident(c.Request.Context(), id, input.Version, action, input.SuppressedUntil, principal, time.Now().UTC())
	platformAlertRespond(c, result, err)
}

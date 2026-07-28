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

type EmailHandler struct{ service *service.EmailService }

type EmailDestinationResponse = monitorModels.EmailDestination
type EmailDeliveryResponse = monitorModels.EmailDelivery

type EmailOperationResponse struct {
	Message string `json:"message" example:"邮件目标已删除"`
}

type CreateEmailDestinationRequest struct {
	Name       string   `json:"name" binding:"required" example:"值班邮箱"`
	Recipients []string `json:"recipients" binding:"required" example:"ops@example.com,oncall@example.com"`
	Enabled    *bool    `json:"enabled,omitempty" example:"true"`
	EventTypes []string `json:"event_types" binding:"required" example:"opened,escalated,resolved"`
}

type UpdateEmailDestinationRequest struct {
	Name       *string   `json:"name,omitempty" example:"值班邮箱"`
	Recipients *[]string `json:"recipients,omitempty" example:"ops@example.com,oncall@example.com"`
	Enabled    *bool     `json:"enabled,omitempty" example:"true"`
	EventTypes *[]string `json:"event_types,omitempty" example:"opened,escalated,resolved"`
}

func NewEmailHandler(emailService *service.EmailService) *EmailHandler {
	return &EmailHandler{service: emailService}
}

// ListEmailDestinations 查询邮件目标
// @Summary 查询邮件目标 | List email destinations
// @Description 查询当前租户的告警邮件目标；SMTP Relay 配置不属于租户 API | List alert email destinations for the current tenant; SMTP Relay settings are not part of the tenant API
// @Tags Monitor Emails
// @Produce json
// @Success 200 {array} EmailDestinationResponse "邮件目标列表 | Email destination list"
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.notification_destination.read"]
// @Router /email-destinations [get]
// @Security BearerAuth
func (h *EmailHandler) ListEmailDestinations(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	destinations, err := h.service.ListDestinations(c.Request.Context(), tenantID)
	if err != nil {
		emailError(c, err)
		return
	}
	c.JSON(http.StatusOK, destinations)
}

// CreateEmailDestination 创建邮件目标
// @Summary 创建邮件目标 | Create email destination
// @Description 创建当前租户的告警邮件目标；只保存收件人和订阅事件 | Create an alert email destination for the current tenant; only recipients and event subscriptions are stored
// @Tags Monitor Emails
// @Accept json
// @Produce json
// @Param request body CreateEmailDestinationRequest true "邮件目标 | Email destination"
// @Success 201 {object} EmailDestinationResponse "已创建目标 | Created destination"
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.notification_destination.create"]
// @Router /email-destinations [post]
// @Security BearerAuth
func (h *EmailHandler) CreateEmailDestination(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	var request CreateEmailDestinationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidEmailDestination)})
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	destination, err := h.service.CreateDestination(c.Request.Context(), service.CreateEmailDestinationInput{
		TenantID: tenantID, Name: request.Name, Recipients: request.Recipients,
		Enabled: enabled, EventTypes: request.EventTypes,
	})
	if err != nil {
		emailError(c, err)
		return
	}
	c.JSON(http.StatusCreated, destination)
}

// UpdateEmailDestination 更新邮件目标
// @Summary 更新邮件目标 | Update email destination
// @Description 部分更新当前租户的邮件目标 | Partially update an email destination for the current tenant
// @Tags Monitor Emails
// @Accept json
// @Produce json
// @Param id path int true "邮件目标 ID | Email destination ID"
// @Param request body UpdateEmailDestinationRequest true "更新内容 | Update fields"
// @Success 200 {object} EmailDestinationResponse "已更新目标 | Updated destination"
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.notification_destination.update"]
// @Router /email-destinations/{id} [patch]
// @Security BearerAuth
func (h *EmailHandler) UpdateEmailDestination(c *gin.Context) {
	id, tenantID, ok := emailIdentity(c)
	if !ok {
		return
	}
	var request UpdateEmailDestinationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidEmailDestination)})
		return
	}
	destination, err := h.service.UpdateDestination(c.Request.Context(), service.UpdateEmailDestinationInput{
		TenantID: tenantID, ID: id, Name: request.Name, Recipients: request.Recipients,
		Enabled: request.Enabled, EventTypes: request.EventTypes,
	})
	if err != nil {
		emailError(c, err)
		return
	}
	c.JSON(http.StatusOK, destination)
}

// TestEmailDestination 测试邮件目标
// @Summary 测试邮件目标 | Test email destination
// @Description 使用目标当前收件人和平台 SMTP Relay 同步发送独立测试邮件；不创建告警事件或正式投递记录 | Send a standalone test email synchronously using the destination's current recipients and the platform SMTP Relay; no alert event or delivery record is created
// @Tags Monitor Emails
// @Produce json
// @Param id path int true "邮件目标 ID | Email destination ID"
// @Success 200 {object} service.EmailTestResult "测试投递成功 | Test delivery succeeded"
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse "SMTP 投递失败 | SMTP delivery failed"
// @Failure 503 {object} ErrorResponse "SMTP 未配置 | SMTP is not configured"
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.notification_destination.execute"]
// @Router /email-destinations/{id}/test [post]
// @Security BearerAuth
func (h *EmailHandler) TestEmailDestination(c *gin.Context) {
	id, tenantID, ok := emailIdentity(c)
	if !ok {
		return
	}
	result, err := h.service.TestDestination(c.Request.Context(), tenantID, id, time.Now())
	if err != nil {
		emailError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteEmailDestination 删除邮件目标
// @Summary 删除邮件目标 | Delete email destination
// @Description 删除当前租户目标并取消尚未领取的邮件投递；历史投递审计保留 | Delete a destination and cancel its unclaimed email deliveries; historical delivery audit is preserved
// @Tags Monitor Emails
// @Produce json
// @Param id path int true "邮件目标 ID | Email destination ID"
// @Success 200 {object} EmailOperationResponse "目标已删除 | Destination deleted"
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.notification_destination.delete"]
// @Router /email-destinations/{id} [delete]
// @Security BearerAuth
func (h *EmailHandler) DeleteEmailDestination(c *gin.Context) {
	id, tenantID, ok := emailIdentity(c)
	if !ok {
		return
	}
	if err := h.service.DeleteDestination(c.Request.Context(), tenantID, id, time.Now()); err != nil {
		emailError(c, err)
		return
	}
	c.JSON(http.StatusOK, EmailOperationResponse{Message: commoni18n.T(c, moni18n.MsgEmailDestinationDeleted)})
}

// ListEmailDeliveries 查询邮件投递
// @Summary 查询邮件投递 | List email deliveries
// @Description 分页查询当前租户的邮件 outbox 和投递审计记录 | List email outbox and delivery audit records for the current tenant
// @Tags Monitor Emails
// @Produce json
// @Param destination_id query int false "邮件目标 ID | Email destination ID"
// @Param status query string false "投递状态 | Delivery status"
// @Param event_type query string false "生命周期事件类型 | Lifecycle event type"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} service.ListEmailDeliveriesResponse "投递记录 | Delivery records"
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.notification_delivery.read"]
// @Router /email-deliveries [get]
// @Security BearerAuth
func (h *EmailHandler) ListEmailDeliveries(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	destinationID, _ := strconv.ParseUint(c.Query("destination_id"), 10, 64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	deliveries, err := h.service.ListDeliveries(c.Request.Context(), service.ListEmailDeliveriesRequest{
		TenantID: tenantID, DestinationID: uint(destinationID), Status: c.Query("status"),
		EventType: c.Query("event_type"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		emailError(c, err)
		return
	}
	c.JSON(http.StatusOK, deliveries)
}

// RetryEmailDelivery 手动重投邮件 delivery
// @Summary 手动重投邮件 delivery | Manually retry email delivery
// @Description 只允许重投 dead delivery；复用原 delivery_id、主题和正文，并使用目标当前收件人开启新的尝试周期 | Retry only a dead delivery; reuse its delivery_id, subject, and body with the destination's current recipients in a new attempt cycle
// @Tags Monitor Emails
// @Produce json
// @Param delivery_id path string true "邮件投递 ID | Email delivery ID"
// @Success 200 {object} EmailDeliveryResponse "已重新入队 | Requeued delivery"
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.notification_delivery.retry"]
// @Router /email-deliveries/{delivery_id}/retry [post]
// @Security BearerAuth
func (h *EmailHandler) RetryEmailDelivery(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	delivery, err := h.service.RetryDelivery(c.Request.Context(), tenantID, c.Param("delivery_id"), time.Now())
	if err != nil {
		emailError(c, err)
		return
	}
	c.JSON(http.StatusOK, delivery)
}

func emailIdentity(c *gin.Context) (uint, int, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidEmailDestination)})
		return 0, 0, false
	}
	tenantID, ok := requireTenantID(c)
	return uint(id), tenantID, ok
}

func emailError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrEmailDestinationInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidEmailDestination)})
	case errors.Is(err, service.ErrEmailDestinationNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgEmailDestinationNotFound)})
	case errors.Is(err, service.ErrEmailDestinationConflict):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, moni18n.MsgEmailDestinationConflict)})
	case errors.Is(err, service.ErrEmailDeliveryInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidEmailDelivery)})
	case errors.Is(err, service.ErrEmailDeliveryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgEmailDeliveryNotFound)})
	case errors.Is(err, service.ErrEmailDeliveryNotRetryable):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, moni18n.MsgEmailDeliveryNotRetryable)})
	case errors.Is(err, service.ErrEmailSenderUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, moni18n.MsgEmailSenderUnavailable)})
	case errors.Is(err, service.ErrEmailTestFailed):
		c.JSON(http.StatusBadGateway, gin.H{"error": commoni18n.T(c, moni18n.MsgEmailTestFailed)})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, moni18n.MsgEmailOperationFailed)})
	}
}

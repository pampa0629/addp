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

type WebhookHandler struct{ service *service.WebhookService }

type WebhookDestinationResponse = monitorModels.WebhookDestination
type WebhookDeliveryResponse = monitorModels.WebhookDelivery

type WebhookOperationResponse struct {
	Message string `json:"message" example:"Webhook 目标已删除"`
}

type CreateWebhookDestinationRequest struct {
	Name       string   `json:"name" binding:"required" example:"运维平台"`
	URL        string   `json:"url" binding:"required" example:"https://ops.example.com/hooks/addp"`
	Secret     string   `json:"secret" binding:"required" example:"replace-with-a-random-secret"`
	Enabled    *bool    `json:"enabled,omitempty" example:"true"`
	EventTypes []string `json:"event_types" binding:"required" example:"opened,resolved"`
}

type UpdateWebhookDestinationRequest struct {
	Name       *string   `json:"name,omitempty" example:"运维平台"`
	URL        *string   `json:"url,omitempty" example:"https://ops.example.com/hooks/addp"`
	Secret     *string   `json:"secret,omitempty" example:"replace-with-a-new-random-secret"`
	Enabled    *bool     `json:"enabled,omitempty" example:"true"`
	EventTypes *[]string `json:"event_types,omitempty" example:"opened,escalated,resolved"`
}

func NewWebhookHandler(webhookService *service.WebhookService) *WebhookHandler {
	return &WebhookHandler{service: webhookService}
}

// ListWebhookDestinations 查询 Webhook 目标
// @Summary 查询 Webhook 目标 | List webhook destinations
// @Description 查询当前租户的告警 Webhook 目标；secret 只返回是否已配置，不返回明文或密文 | List alert webhook destinations for the current tenant; secrets are never returned
// @Tags Monitor Webhooks
// @Produce json
// @Success 200 {array} WebhookDestinationResponse "Webhook 目标列表 | Webhook destination list"
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /webhook-destinations [get]
// @Security BearerAuth
func (h *WebhookHandler) ListWebhookDestinations(c *gin.Context) {
	tenantID, ok := alertTenantID(c)
	if !ok {
		return
	}
	destinations, err := h.service.ListDestinations(c.Request.Context(), tenantID)
	if err != nil {
		webhookError(c, err)
		return
	}
	c.JSON(http.StatusOK, destinations)
}

// CreateWebhookDestination 创建 Webhook 目标
// @Summary 创建 Webhook 目标 | Create webhook destination
// @Description 创建当前租户的 HMAC 签名 Webhook 目标；secret 使用平台加密密钥保存且不会返回 | Create an HMAC-signed webhook destination; the secret is encrypted and never returned
// @Tags Monitor Webhooks
// @Accept json
// @Produce json
// @Param request body CreateWebhookDestinationRequest true "Webhook 目标 | Webhook destination"
// @Success 201 {object} WebhookDestinationResponse "已创建目标 | Created destination"
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /webhook-destinations [post]
// @Security BearerAuth
func (h *WebhookHandler) CreateWebhookDestination(c *gin.Context) {
	tenantID, ok := alertTenantID(c)
	if !ok {
		return
	}
	var request CreateWebhookDestinationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidWebhookDestination)})
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	destination, err := h.service.CreateDestination(c.Request.Context(), service.CreateWebhookDestinationInput{
		TenantID: tenantID, Name: request.Name, URL: request.URL, Secret: request.Secret,
		Enabled: enabled, EventTypes: request.EventTypes,
	})
	if err != nil {
		webhookError(c, err)
		return
	}
	c.JSON(http.StatusCreated, destination)
}

// UpdateWebhookDestination 更新 Webhook 目标
// @Summary 更新 Webhook 目标 | Update webhook destination
// @Description 部分更新当前租户的 Webhook 目标；省略 secret 表示保留现有 secret | Partially update a webhook destination; omit secret to keep the current secret
// @Tags Monitor Webhooks
// @Accept json
// @Produce json
// @Param id path int true "Webhook 目标 ID | Webhook destination ID"
// @Param request body UpdateWebhookDestinationRequest true "更新内容 | Update fields"
// @Success 200 {object} WebhookDestinationResponse "已更新目标 | Updated destination"
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /webhook-destinations/{id} [patch]
// @Security BearerAuth
func (h *WebhookHandler) UpdateWebhookDestination(c *gin.Context) {
	id, tenantID, ok := webhookIdentity(c)
	if !ok {
		return
	}
	var request UpdateWebhookDestinationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidWebhookDestination)})
		return
	}
	destination, err := h.service.UpdateDestination(c.Request.Context(), service.UpdateWebhookDestinationInput{
		TenantID: tenantID, ID: id, Name: request.Name, URL: request.URL, Secret: request.Secret,
		Enabled: request.Enabled, EventTypes: request.EventTypes,
	})
	if err != nil {
		webhookError(c, err)
		return
	}
	c.JSON(http.StatusOK, destination)
}

// TestWebhookDestination 测试 Webhook 目标
// @Summary 测试 Webhook 目标 | Test webhook destination
// @Description 使用目标当前 URL 和 secret 同步发送独立测试 payload；不创建告警事件或正式投递记录 | Send a standalone test payload synchronously with the destination's current URL and secret; no alert event or delivery record is created
// @Tags Monitor Webhooks
// @Produce json
// @Param id path int true "Webhook 目标 ID | Webhook destination ID"
// @Success 200 {object} service.WebhookTestResult "测试投递成功 | Test delivery succeeded"
// @Failure 400 {object} ErrorResponse "目标 ID 无效 | Invalid destination ID"
// @Failure 404 {object} ErrorResponse "目标不存在 | Destination not found"
// @Failure 502 {object} ErrorResponse "接收端投递失败 | Receiver delivery failed"
// @Failure 500 {object} ErrorResponse "内部错误 | Internal error"
// @Router /webhook-destinations/{id}/test [post]
// @Security BearerAuth
func (h *WebhookHandler) TestWebhookDestination(c *gin.Context) {
	id, tenantID, ok := webhookIdentity(c)
	if !ok {
		return
	}
	result, err := h.service.TestDestination(c.Request.Context(), tenantID, id, time.Now())
	if err != nil {
		webhookError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteWebhookDestination 删除 Webhook 目标
// @Summary 删除 Webhook 目标 | Delete webhook destination
// @Description 删除当前租户目标并取消尚未领取的投递；历史投递审计保留 | Delete a destination and cancel its unclaimed deliveries; historical delivery audit is preserved
// @Tags Monitor Webhooks
// @Produce json
// @Param id path int true "Webhook 目标 ID | Webhook destination ID"
// @Success 200 {object} WebhookOperationResponse "目标已删除 | Destination deleted"
// @Failure 400 {object} ErrorResponse "目标 ID 无效 | Invalid destination ID"
// @Failure 404 {object} ErrorResponse "目标不存在 | Destination not found"
// @Failure 500 {object} ErrorResponse "内部错误 | Internal error"
// @Router /webhook-destinations/{id} [delete]
// @Security BearerAuth
func (h *WebhookHandler) DeleteWebhookDestination(c *gin.Context) {
	id, tenantID, ok := webhookIdentity(c)
	if !ok {
		return
	}
	if err := h.service.DeleteDestination(c.Request.Context(), tenantID, id, time.Now()); err != nil {
		webhookError(c, err)
		return
	}
	c.JSON(http.StatusOK, WebhookOperationResponse{Message: commoni18n.T(c, moni18n.MsgWebhookDestinationDeleted)})
}

// ListWebhookDeliveries 查询 Webhook 投递
// @Summary 查询 Webhook 投递 | List webhook deliveries
// @Description 分页查询当前租户的 Webhook outbox 和投递审计记录 | List webhook outbox and delivery audit records for the current tenant
// @Tags Monitor Webhooks
// @Produce json
// @Param destination_id query int false "Webhook 目标 ID | Webhook destination ID"
// @Param status query string false "投递状态 | Delivery status"
// @Param event_type query string false "生命周期事件类型 | Lifecycle event type"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} service.ListWebhookDeliveriesResponse "投递记录 | Delivery records"
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /webhook-deliveries [get]
// @Security BearerAuth
func (h *WebhookHandler) ListWebhookDeliveries(c *gin.Context) {
	tenantID, ok := alertTenantID(c)
	if !ok {
		return
	}
	destinationID, _ := strconv.ParseUint(c.Query("destination_id"), 10, 64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	deliveries, err := h.service.ListDeliveries(c.Request.Context(), service.ListWebhookDeliveriesRequest{
		TenantID: tenantID, DestinationID: uint(destinationID), Status: c.Query("status"),
		EventType: c.Query("event_type"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		webhookError(c, err)
		return
	}
	c.JSON(http.StatusOK, deliveries)
}

// RetryWebhookDelivery 手动重投 Webhook delivery
// @Summary 手动重投 Webhook delivery | Manually retry webhook delivery
// @Description 只允许重投 dead delivery；复用原 delivery_id 和 payload，并使用目标当前 URL/secret 开启新的尝试周期 | Retry only a dead delivery; reuse its delivery_id and payload with the destination's current URL and secret in a new attempt cycle
// @Tags Monitor Webhooks
// @Produce json
// @Param delivery_id path string true "Webhook 投递 ID | Webhook delivery ID"
// @Success 200 {object} WebhookDeliveryResponse "已重新入队 | Requeued delivery"
// @Failure 400 {object} ErrorResponse "投递 ID 无效 | Invalid delivery ID"
// @Failure 404 {object} ErrorResponse "投递不存在 | Delivery not found"
// @Failure 409 {object} ErrorResponse "投递或目标当前不可重投 | Delivery or destination is not retryable"
// @Failure 500 {object} ErrorResponse "内部错误 | Internal error"
// @Router /webhook-deliveries/{delivery_id}/retry [post]
// @Security BearerAuth
func (h *WebhookHandler) RetryWebhookDelivery(c *gin.Context) {
	tenantID, ok := alertTenantID(c)
	if !ok {
		return
	}
	delivery, err := h.service.RetryDelivery(c.Request.Context(), tenantID, c.Param("delivery_id"), time.Now())
	if err != nil {
		webhookError(c, err)
		return
	}
	c.JSON(http.StatusOK, delivery)
}

func webhookIdentity(c *gin.Context) (uint, int, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidWebhookDestination)})
		return 0, 0, false
	}
	tenantID, ok := alertTenantID(c)
	return uint(id), tenantID, ok
}

func webhookError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrWebhookDestinationInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidWebhookDestination)})
	case errors.Is(err, service.ErrWebhookDestinationNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgWebhookDestinationNotFound)})
	case errors.Is(err, service.ErrWebhookDestinationConflict):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, moni18n.MsgWebhookDestinationConflict)})
	case errors.Is(err, service.ErrWebhookDeliveryInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidWebhookDelivery)})
	case errors.Is(err, service.ErrWebhookDeliveryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgWebhookDeliveryNotFound)})
	case errors.Is(err, service.ErrWebhookDeliveryNotRetryable):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, moni18n.MsgWebhookDeliveryNotRetryable)})
	case errors.Is(err, service.ErrWebhookTestFailed):
		c.JSON(http.StatusBadGateway, gin.H{"error": commoni18n.T(c, moni18n.MsgWebhookTestFailed)})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, moni18n.MsgWebhookOperationFailed)})
	}
}

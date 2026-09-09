package api

import (
	"net/http"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type InternalHandler struct {
	apiConsumerService *service.APIConsumerService
}

func NewInternalHandler(apiConsumerService *service.APIConsumerService) *InternalHandler {
	return &InternalHandler{apiConsumerService: apiConsumerService}
}

// ValidateAPIConsumerCredential godoc
// @Summary      验证 API 消费凭据 Hash | Validate API consumer credential hash
// @Description  Gateway 或 Service 平台 Service Principal 验证数据面 API 消费凭据；服务间认证本身仍只使用 Bearer | Gateway or Service validates a data-plane API consumer credential; service-to-service authentication remains Bearer-only
// @Tags         API 消费方 | API Consumers
// @Produce      json
// @Security     BearerAuth
// @Param        key_hash query string true "API Key Hash"
// @Success      200 {object} models.APIConsumerCredentialValidationResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.api_consumer_runtime.read"]
// @Router       /runtime/api-consumer-credentials/validate [get]
func (h *InternalHandler) ValidateAPIConsumerCredential(c *gin.Context) {
	keyHash := c.Query("key_hash")
	if keyHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key_hash is required"})
		return
	}

	var response *models.APIConsumerCredentialValidationResponse
	response, err := h.apiConsumerService.ValidateCredential(keyHash)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

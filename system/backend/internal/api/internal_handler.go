package api

import (
	"net/http"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type InternalHandler struct {
	appService *service.ApplicationService
}

func NewInternalHandler(appService *service.ApplicationService) *InternalHandler {
	return &InternalHandler{appService: appService}
}

// ValidateAPIKeyService godoc
// @Summary      验证外部 API Key Hash | Validate external API key hash
// @Description  Gateway 平台 Service Principal 验证外部请求携带的 API Key Hash；服务间认证本身仍只使用 Bearer | The Gateway platform service principal validates an external API key hash; service-to-service authentication itself remains Bearer-only
// @Tags         应用 API Key | Application API Keys
// @Produce      json
// @Security     BearerAuth
// @Param        key_hash query string true "API Key Hash"
// @Success      200 {object} models.APIKeyValidationResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.api_key.read"]
// @Router       /runtime/api-keys/validate [get]
func (h *InternalHandler) ValidateAPIKeyService(c *gin.Context) {
	keyHash := c.Query("key_hash")
	if keyHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key_hash is required"})
		return
	}

	var response *models.APIKeyValidationResponse
	response, err := h.appService.ValidateAPIKey(keyHash)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

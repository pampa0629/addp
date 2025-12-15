package api

import (
	"net/http"

	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type InternalHandler struct {
	appService *service.ApplicationService
}

func NewInternalHandler(appService *service.ApplicationService) *InternalHandler {
	return &InternalHandler{appService: appService}
}

// ValidateAPIKey 验证 API Key（仅供 Gateway 内部调用）
// GET /internal/api-keys/validate?key_hash={hash}
func (h *InternalHandler) ValidateAPIKey(c *gin.Context) {
	keyHash := c.Query("key_hash")
	if keyHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key_hash is required"})
		return
	}

	response, err := h.appService.ValidateAPIKey(keyHash)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

// BulkGetAPIKeys 批量获取 API Keys（供 Gateway 启动时预加载）
// GET /internal/api-keys/bulk
func (h *InternalHandler) BulkGetAPIKeys(c *gin.Context) {
	// TODO: 实现批量获取逻辑（Phase 2）
	// 这里暂时返回空数组,Gateway 会逐个验证
	c.JSON(http.StatusOK, []interface{}{})
}

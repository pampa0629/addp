package api

import (
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type ConfigHandler struct {
	service *service.BaseMapProviderService
}

func NewConfigHandler(value *service.BaseMapProviderService) *ConfigHandler {
	return &ConfigHandler{service: value}
}

// GetMapConfig 返回地图服务相关配置
// @Summary 获取地图服务配置 | Get map service configuration
// @Description 返回地图服务相关的配置信息（高德地图Key、天地图Key等）| Return map service configuration (AMap key, TDT key, etc.)
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Success 200 {object} map[string]interface{} "地图配置信息 | Map configuration"
// @x-addp-auth-mode "authenticated"
// @Router /config/map [get]
// @Security BearerAuth
func (h *ConfigHandler) GetMapConfig(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "tenant context is required"})
		return
	}
	value, err := h.service.ResolvePublic(c.Request.Context(), uint(tenantID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, value)
}

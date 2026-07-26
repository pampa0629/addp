package api

import (
	"net/http"

	"github.com/addp/manager/internal/config"
	"github.com/gin-gonic/gin"
)

type ConfigHandler struct {
	cfg *config.Config
}

func NewConfigHandler(cfg *config.Config) *ConfigHandler {
	return &ConfigHandler{cfg: cfg}
}

// GetMapConfig 返回地图服务相关配置
// @Summary 获取地图服务配置 | Get map service configuration
// @Description 返回地图服务相关的配置信息（高德地图Key、天地图Key等）| Return map service configuration (AMap key, TDT key, etc.)
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{} "地图配置信息 | Map configuration"
// @x-addp-auth-mode "authenticated"
// @Router /config/map [get]
// @Security BearerAuth
func (h *ConfigHandler) GetMapConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"amap_key":              h.cfg.AMapKey,
		"amap_security_js_code": h.cfg.AMapSecurityJsCode,
		"tdt_key":               h.cfg.TDTKey,
	})
}

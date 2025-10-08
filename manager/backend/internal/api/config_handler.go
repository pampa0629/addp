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
func (h *ConfigHandler) GetMapConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"amap_key":              h.cfg.AMapKey,
		"amap_security_js_code": h.cfg.AMapSecurityJsCode,
		"tdt_key":               h.cfg.TDTKey,
	})
}

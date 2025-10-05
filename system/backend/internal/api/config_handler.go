package api

import (
	"github.com/addp/system/internal/config"
	"github.com/gin-gonic/gin"
)

type ConfigHandler struct {
	cfg *config.Config
}

func NewConfigHandler(cfg *config.Config) *ConfigHandler {
	return &ConfigHandler{cfg: cfg}
}

// GetSharedConfig 返回跨服务共享的配置
// 这是一个内部 API，仅供其他服务启动时调用
func (h *ConfigHandler) GetSharedConfig(c *gin.Context) {
	// 可选：添加内部服务认证（如 API Key）
	apiKey := c.GetHeader("X-Internal-API-Key")
	expectedKey := h.cfg.InternalAPIKey

	if expectedKey != "" && apiKey != expectedKey {
		c.JSON(401, gin.H{"error": "unauthorized: invalid internal API key"})
		return
	}

	c.JSON(200, gin.H{
		"jwt_secret": h.cfg.JWTSecret,
		"database": gin.H{
			"host":     h.cfg.PostgresHost,
			"port":     h.cfg.PostgresPort,
			"user":     h.cfg.PostgresUser,
			"password": h.cfg.PostgresPassword,
			"name":     h.cfg.PostgresDB,
		},
		"encryption_key": h.cfg.EncryptionKey,
	})
}

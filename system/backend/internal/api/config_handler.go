package api

import (
    "encoding/base64"
    "net/http"

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
    encryptionKey := ""
    if len(h.cfg.EncryptionKey) > 0 {
        encryptionKey = base64.StdEncoding.EncodeToString(h.cfg.EncryptionKey)
    }

    c.JSON(http.StatusOK, gin.H{
        "project": gin.H{
            "name": h.cfg.ProjectName,
        },
        "jwt_secret": h.cfg.JWTSecret,
        "encryption_key": encryptionKey,
        "database": gin.H{
            "host":     h.cfg.PostgresHost,
            "port":     h.cfg.PostgresPort,
            "user":     h.cfg.PostgresUser,
            "password": h.cfg.PostgresPassword,
            "name":     h.cfg.PostgresDB,
        },
    })
}

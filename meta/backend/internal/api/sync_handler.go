package api

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/client"
	"github.com/addp/meta/internal/middleware"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

type SyncHandler struct {
	syncService      *service.SyncService
	systemServiceURL string
}

func NewSyncHandler(syncService *service.SyncService, systemServiceURL string) *SyncHandler {
	return &SyncHandler{
		syncService:      syncService,
		systemServiceURL: systemServiceURL,
	}
}

// extractToken 从请求头中提取token
func (h *SyncHandler) extractToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && parts[0] == "Bearer" {
		return parts[1]
	}
	return ""
}

// AutoSyncAll 自动同步所有数据源
// POST /api/meta/sync/auto
func (h *SyncHandler) AutoSyncAll(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	token := h.extractToken(c)

	// 为每个请求创建带token的SystemClient
	systemClient := client.NewSystemClient(h.systemServiceURL, token)
	syncService := service.NewSyncService(h.syncService.GetDB(), systemClient)

	if err := syncService.AutoSyncAll(tenantID); err != nil {
		log.Printf("AutoSyncAll error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("AutoSyncAll started successfully for tenant %d", tenantID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Auto sync started for all data sources",
	})
}

// SyncResource 同步单个资源
// POST /api/meta/sync/:resource_id
func (h *SyncHandler) SyncResource(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	token := h.extractToken(c)

	resourceIDStr := c.Param("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	// 为每个请求创建带token的SystemClient
	systemClient := client.NewSystemClient(h.systemServiceURL, token)
	syncService := service.NewSyncService(h.syncService.GetDB(), systemClient)

	if err := syncService.SyncResource(uint(resourceID), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Resource sync completed successfully",
	})
}

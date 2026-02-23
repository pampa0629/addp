package api

import (
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/portal/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func SetupRouter(cfg *config.Config, redisClient *redis.Client) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 健康检查（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "module": "portal"})
	})

	// Portal BFF 路由（需要认证）
	api := router.Group("/api/portal")
	if redisClient != nil {
		api.Use(commonAuth.CachedSystemAuthMiddleware(cfg.SystemURL, redisClient, 5*time.Minute))
	} else {
		api.Use(commonAuth.SystemAuthMiddleware(cfg.SystemURL))
	}

	// ============================================================
	// 门户首页（Phase 3 实现）
	// ============================================================
	api.GET("/home", placeholderHandler("portal home data"))

	// ============================================================
	// 资产发现（Phase 3 实现）
	// ============================================================
	api.GET("/search", placeholderHandler("portal search"))
	api.GET("/catalogs", placeholderHandler("portal categories"))
	api.GET("/catalogs/:id/assets", placeholderHandler("portal category assets"))
	api.GET("/assets", placeholderHandler("portal asset list"))
	api.GET("/assets/:id", placeholderHandler("portal asset detail"))

	// ============================================================
	// 资产申请（Phase 4 实现）
	// ============================================================
	api.POST("/assets/:id/apply", placeholderHandler("portal apply asset"))
	api.GET("/my/applications", placeholderHandler("portal my applications"))

	// ============================================================
	// 我的授权（Phase 5 实现）
	// ============================================================
	api.GET("/my/authorizations", placeholderHandler("portal my authorizations"))

	// ============================================================
	// 资产评价（Phase 6 实现）
	// ============================================================
	api.POST("/assets/:id/ratings", placeholderHandler("portal create rating"))
	api.PUT("/ratings/:id", placeholderHandler("portal update rating"))

	return router
}

// placeholderHandler 骨架占位处理器
func placeholderHandler(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(501, gin.H{
			"message": "not implemented yet",
			"action":  action,
		})
	}
}

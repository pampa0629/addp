package api

import (
	"io"
	"net/http"
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// proxyToStandard 创建一个代理处理器，将请求转发到 Standard 模块
func proxyToStandard(standardURL, targetPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 构建目标 URL
		url := standardURL + targetPath
		if c.Param("path") != "" {
			url += c.Param("path")
		}

		// 创建新请求
		req, err := http.NewRequest(c.Request.Method, url, c.Request.Body)
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to create proxy request"})
			return
		}

		// 复制请求头（包括 Authorization）
		for key, values := range c.Request.Header {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}

		// 复制查询参数
		req.URL.RawQuery = c.Request.URL.RawQuery

		// 发送请求
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(502, gin.H{"error": "failed to proxy request to standard service"})
			return
		}
		defer resp.Body.Close()

		// 复制响应头
		for key, values := range resp.Header {
			for _, value := range values {
				c.Writer.Header().Add(key, value)
			}
		}

		// 设置状态码
		c.Status(resp.StatusCode)

		// 复制响应体
		io.Copy(c.Writer, resp.Body)
	}
}

// getTenantID 从 context 获取租户 ID
func getTenantID(c *gin.Context) int64 {
	if tenantID, exists := c.Get("tenant_id"); exists {
		if id, ok := tenantID.(int64); ok {
			return id
		}
	}
	return 1 // 默认租户
}

// getUserID 从 context 获取用户 ID
func getUserID(c *gin.Context) int64 {
	if userID, exists := c.Get("user_id"); exists {
		if id, ok := userID.(int64); ok {
			return id
		}
	}
	return 1
}

// SetupRouter 设置路由（仅 Model 相关）
func SetupRouter(
	entitySvc *service.EntityService,
	entityRelationSvc *service.EntityRelationService,
	logicalTableSvc *service.LogicalTableService,
	dwLayerSvc *service.DWLayerService,
	factMetricSvc *service.FactMetricService,
	tableRelationSvc *service.TableRelationService,
	systemURL string,
	standardURL string,
	redisClient *redis.Client,
) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// 创建 Handlers（仅 Model 相关）
	entityHandler := NewEntityHandler(entitySvc)
	entityRelationHandler := NewEntityRelationHandler(entityRelationSvc)
	logicalTableHandler := NewLogicalTableHandler(logicalTableSvc)
	dwLayerHandler := NewDWLayerHandler(dwLayerSvc)
	factMetricHandler := NewFactMetricHandler(factMetricSvc)
	tableRelationHandler := NewTableRelationHandler(tableRelationSvc)

	// API 路由组
	api := router.Group("/api/model")

	// 使用 Redis 缓存中间件 (TTL: 5分钟)
	if redisClient != nil {
		api.Use(commonAuth.CachedSystemAuthMiddleware(systemURL, redisClient, 5*time.Minute))
	} else {
		api.Use(commonAuth.SystemAuthMiddleware(systemURL))
	}

	{
		// 业务实体路由
		entities := api.Group("/entities")
		{
			entities.GET("", entityHandler.ListEntities)
			entities.POST("", entityHandler.CreateEntity)
			entities.GET("/:id", entityHandler.GetEntity)
			entities.PUT("/:id", entityHandler.UpdateEntity)
			entities.DELETE("/:id", entityHandler.DeleteEntity)
			entities.POST("/:id/approve", entityHandler.ApproveEntity)
			entities.GET("/:id/attributes", entityHandler.GetAttributes)
			entities.POST("/:id/attributes", entityHandler.CreateAttribute)
			entities.PUT("/:id/attributes/:aid", entityHandler.UpdateAttribute)
			entities.DELETE("/:id/attributes/:aid", entityHandler.DeleteAttribute)
			// Mermaid 导入导出
			entities.POST("/import-mermaid", entityHandler.ImportMermaid)
			entities.GET("/export-mermaid", entityHandler.ExportMermaid)
		}

		// 实体关系路由
		entityRelations := api.Group("/entity-relations")
		{
			entityRelations.GET("", entityRelationHandler.ListRelations)
			entityRelations.POST("", entityRelationHandler.CreateRelation)
			entityRelations.GET("/:id", entityRelationHandler.GetRelation)
			entityRelations.PUT("/:id", entityRelationHandler.UpdateRelation)
			entityRelations.DELETE("/:id", entityRelationHandler.DeleteRelation)
		}

		// 逻辑表路由
		logicalTables := api.Group("/logical-tables")
		{
			logicalTables.GET("", logicalTableHandler.ListLogicalTables)
			logicalTables.POST("", logicalTableHandler.CreateLogicalTable)
			logicalTables.GET("/:id", logicalTableHandler.GetLogicalTable)
			logicalTables.PUT("/:id", logicalTableHandler.UpdateLogicalTable)
			logicalTables.DELETE("/:id", logicalTableHandler.DeleteLogicalTable)
			logicalTables.GET("/:id/fields", logicalTableHandler.GetFields)
			logicalTables.POST("/:id/fields", logicalTableHandler.CreateField)
			logicalTables.PUT("/:id/fields/:fid", logicalTableHandler.UpdateField)
			logicalTables.DELETE("/:id/fields/:fid", logicalTableHandler.DeleteField)
			logicalTables.POST("/:id/preview-ddl", logicalTableHandler.PreviewDDL)
			// 事实表关联指标（仅对 table_type='fact' 的表有意义）
			logicalTables.GET("/:id/metrics", factMetricHandler.ListMetrics)
			logicalTables.POST("/:id/metrics", factMetricHandler.AddMetric)
			logicalTables.DELETE("/:id/metrics/:mid", factMetricHandler.RemoveMetric)
			// 事实表关联维度表
			logicalTables.GET("/:id/dimension-relations", tableRelationHandler.ListDimensionRelations)
			logicalTables.POST("/:id/dimension-relations", tableRelationHandler.AddDimensionRelation)
			logicalTables.DELETE("/:id/dimension-relations/:rid", tableRelationHandler.RemoveDimensionRelation)
		}

		// 数仓分层路由
		dwLayers := api.Group("/dw-layers")
		{
			dwLayers.GET("", dwLayerHandler.ListDWLayers)
			dwLayers.POST("", dwLayerHandler.CreateDWLayer)
			dwLayers.GET("/:id", dwLayerHandler.GetDWLayer)
			dwLayers.PUT("/:id", dwLayerHandler.UpdateDWLayer)
			dwLayers.DELETE("/:id", dwLayerHandler.DeleteDWLayer)
		}

		// 代理到 Standard 模块的路由
		// 业务域
		api.Any("/domains", proxyToStandard(standardURL, "/api/standard/domains"))
		api.Any("/domains/*path", proxyToStandard(standardURL, "/api/standard/domains"))

		// 业务术语
		api.Any("/glossaries", proxyToStandard(standardURL, "/api/standard/glossaries"))
		api.Any("/glossaries/*path", proxyToStandard(standardURL, "/api/standard/glossaries"))

		// 数据元
		api.Any("/elements", proxyToStandard(standardURL, "/api/standard/elements"))
		api.Any("/elements/*path", proxyToStandard(standardURL, "/api/standard/elements"))

		// 码值集
		api.Any("/code-sets", proxyToStandard(standardURL, "/api/standard/code-sets"))
		api.Any("/code-sets/*path", proxyToStandard(standardURL, "/api/standard/code-sets"))

		// 维度层级（代理到 Standard 模块）
		api.Any("/dimension-hierarchies", proxyToStandard(standardURL, "/api/standard/dimension-hierarchies"))
		api.Any("/dimension-hierarchies/*path", proxyToStandard(standardURL, "/api/standard/dimension-hierarchies"))

		// 指标（代理到 Standard 模块，用于搜索关联指标）
		api.Any("/standard-metrics", proxyToStandard(standardURL, "/api/standard/metrics"))
		api.Any("/standard-metrics/*path", proxyToStandard(standardURL, "/api/standard/metrics"))
	}

	// 健康检查（无认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return router
}

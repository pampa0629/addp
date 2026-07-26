package api

import (
	"io"
	"net/http"
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/model/docs"
	modelauthorization "github.com/addp/model/internal/authorization"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
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
	return int64(commonAuth.GetTenantID(c))
}

// getUserID 从 context 获取用户 ID
func getUserID(c *gin.Context) int64 {
	return int64(commonAuth.GetUserID(c))
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

	// i18n 中间件（解析 Accept-Language 请求头）
	router.Use(i18nmiddleware.I18nMiddleware())

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

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
	api := router.Group("/api/v1/model")

	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}

	{
		// 业务实体路由
		entities := api.Group("/entities")
		{
			entities.GET("", permission(modelauthorization.PermissionModelEntityRead), entityHandler.ListEntities)
			entities.POST("", permission(modelauthorization.PermissionModelEntityCreate), entityHandler.CreateEntity)
			entities.GET("/:id", permission(modelauthorization.PermissionModelEntityRead), entityHandler.GetEntity)
			entities.PUT("/:id", permission(modelauthorization.PermissionModelEntityUpdate), entityHandler.UpdateEntity)
			entities.DELETE("/:id", permission(modelauthorization.PermissionModelEntityDelete), entityHandler.DeleteEntity)
			entities.POST("/:id/approve", permission(modelauthorization.PermissionModelEntityApprove), entityHandler.ApproveEntity)
			entities.GET("/:id/attributes", permission(modelauthorization.PermissionModelEntityRead), entityHandler.GetAttributes)
			entities.POST("/:id/attributes", permission(modelauthorization.PermissionModelEntityCreate), entityHandler.CreateAttribute)
			entities.PUT("/:id/attributes/:aid", permission(modelauthorization.PermissionModelEntityUpdate), entityHandler.UpdateAttribute)
			entities.DELETE("/:id/attributes/:aid", permission(modelauthorization.PermissionModelEntityDelete), entityHandler.DeleteAttribute)
			// Mermaid 导入导出
			entities.POST("/import-mermaid", permission(modelauthorization.PermissionModelEntityCreate, modelauthorization.PermissionModelEntityRelationCreate), entityHandler.ImportMermaid)
			entities.GET("/export-mermaid", permission(modelauthorization.PermissionModelEntityRead, modelauthorization.PermissionModelEntityRelationRead), entityHandler.ExportMermaid)
		}

		// 实体关系路由
		entityRelations := api.Group("/entity-relations")
		{
			entityRelations.GET("", permission(modelauthorization.PermissionModelEntityRelationRead), entityRelationHandler.ListRelations)
			entityRelations.POST("", permission(modelauthorization.PermissionModelEntityRelationCreate), entityRelationHandler.CreateRelation)
			entityRelations.GET("/:id", permission(modelauthorization.PermissionModelEntityRelationRead), entityRelationHandler.GetRelation)
			entityRelations.PUT("/:id", permission(modelauthorization.PermissionModelEntityRelationUpdate), entityRelationHandler.UpdateRelation)
			entityRelations.DELETE("/:id", permission(modelauthorization.PermissionModelEntityRelationDelete), entityRelationHandler.DeleteRelation)
		}

		// 逻辑表路由
		logicalTables := api.Group("/logical-tables")
		{
			logicalTables.GET("", permission(modelauthorization.PermissionModelLogicalModelRead), logicalTableHandler.ListLogicalTables)
			logicalTables.POST("", permission(modelauthorization.PermissionModelLogicalModelCreate), logicalTableHandler.CreateLogicalTable)
			logicalTables.GET("/:id", permission(modelauthorization.PermissionModelLogicalModelRead), logicalTableHandler.GetLogicalTable)
			logicalTables.PUT("/:id", permission(modelauthorization.PermissionModelLogicalModelUpdate), logicalTableHandler.UpdateLogicalTable)
			logicalTables.DELETE("/:id", permission(modelauthorization.PermissionModelLogicalModelDelete), logicalTableHandler.DeleteLogicalTable)
			logicalTables.GET("/:id/fields", permission(modelauthorization.PermissionModelLogicalModelRead), logicalTableHandler.GetFields)
			logicalTables.POST("/:id/fields", permission(modelauthorization.PermissionModelLogicalModelCreate), logicalTableHandler.CreateField)
			logicalTables.PUT("/:id/fields/:fid", permission(modelauthorization.PermissionModelLogicalModelUpdate), logicalTableHandler.UpdateField)
			logicalTables.DELETE("/:id/fields/:fid", permission(modelauthorization.PermissionModelLogicalModelDelete), logicalTableHandler.DeleteField)
			logicalTables.POST("/:id/preview-ddl", permission(modelauthorization.PermissionModelLogicalModelRead), logicalTableHandler.PreviewDDL)
			// 事实表关联指标（仅对 table_type='fact' 的表有意义）
			logicalTables.GET("/:id/metrics", permission(modelauthorization.PermissionModelLogicalModelRead), factMetricHandler.ListMetrics)
			logicalTables.POST("/:id/metrics", permission(modelauthorization.PermissionModelLogicalModelUpdate), factMetricHandler.AddMetric)
			logicalTables.DELETE("/:id/metrics/:mid", permission(modelauthorization.PermissionModelLogicalModelUpdate), factMetricHandler.RemoveMetric)
			// 事实表关联维度表
			logicalTables.GET("/:id/dimension-relations", permission(modelauthorization.PermissionModelLogicalModelRead), tableRelationHandler.ListDimensionRelations)
			logicalTables.POST("/:id/dimension-relations", permission(modelauthorization.PermissionModelLogicalModelUpdate), tableRelationHandler.AddDimensionRelation)
			logicalTables.DELETE("/:id/dimension-relations/:rid", permission(modelauthorization.PermissionModelLogicalModelUpdate), tableRelationHandler.RemoveDimensionRelation)
		}

		// 数仓分层路由
		dwLayers := api.Group("/dw-layers")
		{
			dwLayers.GET("", permission(modelauthorization.PermissionModelDwLayerRead), dwLayerHandler.ListDWLayers)
			dwLayers.POST("", permission(modelauthorization.PermissionModelDwLayerCreate), dwLayerHandler.CreateDWLayer)
			dwLayers.GET("/:id", permission(modelauthorization.PermissionModelDwLayerRead), dwLayerHandler.GetDWLayer)
			dwLayers.PUT("/:id", permission(modelauthorization.PermissionModelDwLayerUpdate), dwLayerHandler.UpdateDWLayer)
			dwLayers.DELETE("/:id", permission(modelauthorization.PermissionModelDwLayerDelete), dwLayerHandler.DeleteDWLayer)
		}

		// 代理到 Standard 模块的路由
		// 业务域
		api.Any("/domains", proxyToStandard(standardURL, "/api/v1/standard/domains"))
		api.Any("/domains/*path", proxyToStandard(standardURL, "/api/v1/standard/domains"))

		// 业务术语
		api.Any("/glossaries", proxyToStandard(standardURL, "/api/v1/standard/glossaries"))
		api.Any("/glossaries/*path", proxyToStandard(standardURL, "/api/v1/standard/glossaries"))

		// 数据元
		api.Any("/elements", proxyToStandard(standardURL, "/api/v1/standard/elements"))
		api.Any("/elements/*path", proxyToStandard(standardURL, "/api/v1/standard/elements"))

		// 码值集
		api.Any("/code-sets", proxyToStandard(standardURL, "/api/v1/standard/code-sets"))
		api.Any("/code-sets/*path", proxyToStandard(standardURL, "/api/v1/standard/code-sets"))

		// 维度层级（代理到 Standard 模块）
		api.Any("/dimension-hierarchies", proxyToStandard(standardURL, "/api/v1/standard/dimension-hierarchies"))
		api.Any("/dimension-hierarchies/*path", proxyToStandard(standardURL, "/api/v1/standard/dimension-hierarchies"))

		// 指标（代理到 Standard 模块，用于搜索关联指标）
		api.Any("/standard-metrics", proxyToStandard(standardURL, "/api/v1/standard/metrics"))
		api.Any("/standard-metrics/*path", proxyToStandard(standardURL, "/api/v1/standard/metrics"))
	}

	// 健康检查（无认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return router
}

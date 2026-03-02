package api

import (
	"net/http"
	"strconv"
	"time"

	commonAPI "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/asset/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func SetupRouter(db *gorm.DB, systemURL string, redisClient *redis.Client, assetSvc *service.AssetService) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 健康检查（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "module": "asset"})
	})

	// 初始化服务层
	typeSvc := service.NewTypeService(db)
	catalogSvc := service.NewCatalogService(db)
	authorizationSvc := service.NewAuthorizationService(db)
	applicationSvc := service.NewApplicationService(db, authorizationSvc)
	ratingSvc := service.NewRatingService(db)

	// API 路由组（需要认证）
	api := router.Group("/api/asset")
	if redisClient != nil {
		api.Use(commonAuth.CachedSystemAuthMiddleware(systemURL, redisClient, 5*time.Minute))
	} else {
		api.Use(commonAuth.SystemAuthMiddleware(systemURL))
	}

	// ============================================================
	// 资产类型管理
	// ============================================================
	typeGroup := api.Group("/type-definitions")
	{
		typeGroup.GET("", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			types, err := typeSvc.ListTypes(tenantID)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"data": types})
		})

		typeGroup.GET("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			t, err := typeSvc.GetType(id)
			if err != nil {
				commonAPI.NotFoundError(c, "资产类型不存在")
				return
			}
			commonAPI.SuccessResponse(c, t)
		})

		typeGroup.POST("", func(c *gin.Context) {
			c.JSON(http.StatusForbidden, gin.H{"error": "资产类型管理为只读，请通过系统初始化配置"})
		})
		typeGroup.PUT("/:id", func(c *gin.Context) {
			c.JSON(http.StatusForbidden, gin.H{"error": "资产类型管理为只读"})
		})
		typeGroup.DELETE("/:id", func(c *gin.Context) {
			c.JSON(http.StatusForbidden, gin.H{"error": "资产类型管理为只读"})
		})
	}

	// ============================================================
	// 目录管理
	// ============================================================
	catalogGroup := api.Group("/catalogs")
	{
		catalogGroup.GET("", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			cats, err := catalogSvc.ListAll(tenantID)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"data": cats})
		})

		catalogGroup.GET("/tree", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			tree, err := catalogSvc.GetTree(tenantID)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"data": tree})
		})

		catalogGroup.GET("/:id", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			cat, err := catalogSvc.Get(tenantID, id)
			if err != nil {
				commonAPI.NotFoundError(c, "目录不存在")
				return
			}
			commonAPI.SuccessResponse(c, cat)
		})

		catalogGroup.POST("", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			var req service.CreateCatalogReq
			if !commonAPI.BindJSON(c, &req) {
				return
			}
			cat, err := catalogSvc.Create(tenantID, &req)
			if err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.CreatedResponse(c, cat)
		})

		catalogGroup.PUT("/:id", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			var req service.UpdateCatalogReq
			if !commonAPI.BindJSON(c, &req) {
				return
			}
			cat, err := catalogSvc.Update(tenantID, id, &req)
			if err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, cat)
		})

		catalogGroup.DELETE("/:id", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			if err := catalogSvc.Delete(tenantID, id); err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.NoContentResponse(c)
		})
	}

	// ============================================================
	// 资产管理
	// ============================================================
	assetGroup := api.Group("/assets")
	{
		// 列表（支持分页、类型、状态、目录、关键字过滤）
		assetGroup.GET("", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			page, pageSize := commonAPI.GetPaginationParams(c)
			var typeID int64
			if s := c.Query("type_id"); s != "" {
				typeID, _ = strconv.ParseInt(s, 10, 64)
			}
			params := &service.AssetListParams{
				Page:     page,
				PageSize: pageSize,
				Status:   c.Query("status"),
				TypeID:   typeID,
				Keyword:  c.Query("keyword"),
			}
			if catStr := c.Query("catalog_id"); catStr != "" {
				catID, err := strconv.ParseInt(catStr, 10, 64)
				if err == nil {
					params.CatalogID = &catID
				}
			}
			assets, total, err := assetSvc.List(tenantID, params)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SendPaginatedResponse(c, assets, total, page, pageSize)
		})

		// 详情
		assetGroup.GET("/:id", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			asset, err := assetSvc.Get(tenantID, id)
			if err != nil {
				commonAPI.NotFoundError(c, "资产不存在")
				return
			}
			commonAPI.SuccessResponse(c, asset)
		})

		// 更新基本信息（名称/描述/目录/标签）
		assetGroup.PUT("/:id", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			userID := commonAuth.GetUserID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			var req service.UpdateAssetReq
			if !commonAPI.BindJSON(c, &req) {
				return
			}
			asset, err := assetSvc.Update(tenantID, id, userID, &req)
			if err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, asset)
		})

		// 删除（仅草稿）
		assetGroup.DELETE("/:id", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			if err := assetSvc.Delete(tenantID, id); err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.NoContentResponse(c)
		})

		// 上架（draft/offline → published）
		assetGroup.POST("/:id/publish", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			if err := assetSvc.Publish(tenantID, id); err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"message": "资产已上架"})
		})

		// 下架（published → offline）
		assetGroup.POST("/:id/offline", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			if err := assetSvc.Offline(tenantID, id); err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"message": "资产已下架"})
		})

		// 批量上架
		assetGroup.POST("/batch-publish", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			var req service.BatchIDsReq
			if !commonAPI.BindJSON(c, &req) {
				return
			}
			count, err := assetSvc.BatchPublish(tenantID, req.IDs)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"affected": count})
		})

		// 批量下架
		assetGroup.POST("/batch-offline", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			var req service.BatchIDsReq
			if !commonAPI.BindJSON(c, &req) {
				return
			}
			count, err := assetSvc.BatchOffline(tenantID, req.IDs)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"affected": count})
		})

		// 批量归目录
		assetGroup.POST("/batch-catalog", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			var req service.BatchCatalogReq
			if !commonAPI.BindJSON(c, &req) {
				return
			}
			count, err := assetSvc.BatchCatalog(tenantID, req.IDs, req.CatalogID)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"affected": count})
		})

		// 从各源模块同步/发现新资产
		assetGroup.POST("/sync", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			result, err := assetSvc.Sync(tenantID)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, result)
		})

		// 已上架资产统计（各类型数量 + 总计，供 portal 首页使用）
		assetGroup.GET("/stats", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			result, err := assetSvc.GetStats(tenantID)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, result)
		})

		// 运营看板统计（管理员使用）
		assetGroup.GET("/stats/dashboard", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			result, err := assetSvc.GetDashboardStats(tenantID)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, result)
		})

		// 获取类型扩展字段定义（编目表单用）
		assetGroup.GET("/type-fields/:type_id", func(c *gin.Context) {
			typeID, err := strconv.ParseInt(c.Param("type_id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的类型ID")
				return
			}
			schemas, err := assetSvc.GetTypeFieldSchemas(typeID)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"data": schemas})
		})
	}

	// ============================================================
	// 申请管理（Phase 4）
	// ============================================================
	appGroup := api.Group("/applications")
	{
		// GET /api/asset/applications — 列表（管理员视角，支持 display_status/status/asset_id/applicant_id 过滤）
		appGroup.GET("", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			page, pageSize := commonAPI.GetPaginationParams(c)
			params := service.ApplicationListParams{
				Page:          page,
				PageSize:      pageSize,
				Status:        c.Query("status"),
				DisplayStatus: c.Query("display_status"),
			}
			if v := c.Query("asset_id"); v != "" {
				if id, err := strconv.ParseInt(v, 10, 64); err == nil {
					params.AssetID = id
				}
			}
			if v := c.Query("applicant_id"); v != "" {
				if id, err := strconv.ParseInt(v, 10, 64); err == nil {
					params.ApplicantID = id
				}
			}
			items, total, err := applicationSvc.List(tenantID, params)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SendPaginatedResponse(c, items, total, page, pageSize)
		})

		// POST /api/asset/applications — 提交申请（由 portal BFF 代用户调用）
		appGroup.POST("", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			var req service.CreateApplicationReq
			if !commonAPI.BindJSON(c, &req) {
				return
			}
			app, err := applicationSvc.Create(tenantID, &req)
			if err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, app)
		})

		// GET /api/asset/applications/:id — 单条详情
		appGroup.GET("/:id", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			app, err := applicationSvc.Get(tenantID, id)
			if err != nil {
				commonAPI.NotFoundError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, app)
		})

		// POST /api/asset/applications/:id/approve — 审批通过
		appGroup.POST("/:id/approve", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			reviewerID := commonAuth.GetUserID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			var req service.ApproveApplicationReq
			if !commonAPI.BindJSON(c, &req) {
				return
			}
			if err := applicationSvc.Approve(tenantID, reviewerID, id, &req); err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"message": "审批通过"})
		})

		// POST /api/asset/applications/:id/reject — 审批驳回
		appGroup.POST("/:id/reject", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			reviewerID := commonAuth.GetUserID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			var req service.RejectApplicationReq
			if !commonAPI.BindJSON(c, &req) {
				return
			}
			if err := applicationSvc.Reject(tenantID, reviewerID, id, &req); err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"message": "已驳回"})
		})

		// POST /api/asset/applications/:id/revoke — 通过申请ID撤销对应授权
		appGroup.POST("/:id/revoke", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			revokedBy := commonAuth.GetUserID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			if err := applicationSvc.RevokeByApplication(tenantID, revokedBy, id); err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"message": "授权已撤销"})
		})
	}

	// ============================================================
	// 授权管理（Phase 4）
	// ============================================================
	authGroup := api.Group("/authorizations")
	{
		// GET /api/asset/authorizations — 列表（支持 user_id/asset_id/is_active 过滤）
		authGroup.GET("", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			page, pageSize := commonAPI.GetPaginationParams(c)
			params := service.AuthorizationListParams{
				Page:     page,
				PageSize: pageSize,
			}
			if v := c.Query("user_id"); v != "" {
				if id, err := strconv.ParseInt(v, 10, 64); err == nil {
					params.UserID = id
				}
			}
			if v := c.Query("asset_id"); v != "" {
				if id, err := strconv.ParseInt(v, 10, 64); err == nil {
					params.AssetID = id
				}
			}
			if v := c.Query("is_active"); v == "true" {
				t := true
				params.IsActive = &t
			} else if v == "false" {
				f := false
				params.IsActive = &f
			}
			items, total, err := authorizationSvc.List(tenantID, params)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SendPaginatedResponse(c, items, total, page, pageSize)
		})

		// GET /api/asset/authorizations/:id — 单条详情
		authGroup.GET("/:id", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			auth, err := authorizationSvc.Get(tenantID, id)
			if err != nil {
				commonAPI.NotFoundError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, auth)
		})

		// POST /api/asset/authorizations/:id/revoke — 撤销授权
		authGroup.POST("/:id/revoke", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			revokedBy := commonAuth.GetUserID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			if err := authorizationSvc.Revoke(tenantID, revokedBy, id); err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"message": "授权已撤销"})
		})
	}

	// ============================================================
	// 资产评价（Phase 6）
	// ============================================================
	ratingGroup := api.Group("/ratings")
	{
		// GET /api/asset/ratings?asset_id=X&has_feedback=true&is_handled=false
		ratingGroup.GET("", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			page, pageSize := commonAPI.GetPaginationParams(c)
			params := service.RatingListParams{Page: page, PageSize: pageSize}
			if v := c.Query("asset_id"); v != "" {
				if id, err := strconv.ParseInt(v, 10, 64); err == nil {
					params.AssetID = id
				}
			}
			if v := c.Query("user_id"); v != "" {
				if id, err := strconv.ParseInt(v, 10, 64); err == nil {
					params.UserID = id
				}
			}
			if c.Query("has_feedback") == "true" {
				params.HasFeedback = true
			}
			if v := c.Query("is_handled"); v == "true" {
				t := true
				params.IsHandled = &t
			} else if v == "false" {
				f := false
				params.IsHandled = &f
			}
			items, total, err := ratingSvc.List(uint(tenantID), params)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SendPaginatedResponse(c, items, total, page, pageSize)
		})

		// POST /api/asset/ratings — 提交或更新评价（upsert 语义，asset_id 和 user_id 从 body 中读取）
		ratingGroup.POST("", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			var body struct {
				AssetID int64   `json:"asset_id" binding:"required"`
				UserID  int64   `json:"user_id" binding:"required"`
				Score   float32 `json:"score" binding:"required,min=1,max=5"`
				Comment string  `json:"comment"`
				Tags    []string `json:"tags"`
			}
			if !commonAPI.BindJSON(c, &body) {
				return
			}
			req := &service.UpsertRatingReq{Score: body.Score, Comment: body.Comment, Tags: body.Tags}
			rating, err := ratingSvc.Upsert(uint(tenantID), body.UserID, body.AssetID, req)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, rating)
		})

		// POST /api/asset/ratings/:id/mark-handled — 管理员标记问题反馈为已处理
		ratingGroup.POST("/:id/mark-handled", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				commonAPI.BadRequestError(c, "无效的ID")
				return
			}
			var body struct {
				IsHandled bool `json:"is_handled"`
			}
			if !commonAPI.BindJSON(c, &body) {
				return
			}
			if err := ratingSvc.MarkHandled(uint(tenantID), id, body.IsHandled); err != nil {
				commonAPI.BadRequestError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, gin.H{"message": "已更新"})
		})

		// GET /api/asset/ratings/stats?asset_id=X — 获取资产评价统计
		ratingGroup.GET("/stats", func(c *gin.Context) {
			tenantID := commonAuth.GetTenantID(c)
			assetID, err := strconv.ParseInt(c.Query("asset_id"), 10, 64)
			if err != nil || assetID <= 0 {
				commonAPI.BadRequestError(c, "缺少 asset_id 参数")
				return
			}
			stats, err := ratingSvc.GetStats(uint(tenantID), assetID)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}
			commonAPI.SuccessResponse(c, stats)
		})
	}

	return router
}

// placeholderHandler 未实现端点的占位处理器
func placeholderHandler(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(501, gin.H{
			"message": "not implemented yet",
			"action":  action,
		})
	}
}

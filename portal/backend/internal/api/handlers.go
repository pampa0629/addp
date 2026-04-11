package api

import (
	"strconv"

	commonAPI "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commonClient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 门户首页
// ============================================================

// @Summary 获取门户首页数据 | Get portal home data
// @Tags Portal
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /home [get]
// @Security BearerAuth
// handleHome GET /api/portal/home
// 返回最新上架资产（前 6 条）+ 各类型统计数
func handleHome(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))

		// 并发获取：最新上架资产 + 统计数据
		type latestResult struct {
			resp *commonClient.AssetListResponse
			err  error
		}
		type statsResult struct {
			resp *commonClient.AssetStatsResponse
			err  error
		}

		latestCh := make(chan latestResult, 1)
		statsCh := make(chan statsResult, 1)

		go func() {
			page := 1
			pageSize := 6
			resp, err := assetClient.GetAssets(tenantID, commonClient.AssetQueryOptions{
				Status:   commonClient.AssetStatusPublished,
				Page:     page,
				PageSize: pageSize,
			})
			latestCh <- latestResult{resp, err}
		}()

		go func() {
			resp, err := assetClient.GetAssetStats(tenantID)
			statsCh <- statsResult{resp, err}
		}()

		lr := <-latestCh
		sr := <-statsCh

		if lr.err != nil {
			commonAPI.InternalServerError(c, "获取资产列表失败: "+lr.err.Error())
			return
		}
		if sr.err != nil {
			commonAPI.InternalServerError(c, "获取统计数据失败: "+sr.err.Error())
			return
		}

		commonAPI.SuccessResponse(c, gin.H{
			"latest_assets": lr.resp.Items,
			"type_stats":    sr.resp.TypeStats,
			"total_published": sr.resp.Total,
		})
	}
}

// ============================================================
// 目录浏览
// ============================================================

// @Summary 获取目录树 | Get catalog tree
// @Tags Portal
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /catalogs [get]
// @Security BearerAuth
// handleCatalogs GET /api/portal/catalogs
// 返回目录树（只含有 published 资产的节点）
func handleCatalogs(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		catalogs, err := assetClient.GetCatalogs(tenantID)
		if err != nil {
			commonAPI.InternalServerError(c, "获取目录失败: "+err.Error())
			return
		}
		commonAPI.SuccessResponse(c, catalogs)
	}
}

// @Summary 获取目录下的资产列表 | Get assets in catalog
// @Tags Portal
// @Produce json
// @Param id path int true "目录ID | Catalog ID"
// @Success 200 {object} map[string]interface{}
// @Router /catalogs/{id}/assets [get]
// @Security BearerAuth
// handleCatalogAssets GET /api/portal/catalogs/:id/assets
func handleCatalogAssets(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		catalogID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的目录 ID")
			return
		}
		page, pageSize := commonAPI.GetPaginationParams(c)

		resp, err := assetClient.GetAssets(tenantID, commonClient.AssetQueryOptions{
			Status:    commonClient.AssetStatusPublished,
			CatalogID: &catalogID,
			Page:      page,
			PageSize:  pageSize,
		})
		if err != nil {
			commonAPI.InternalServerError(c, "获取资产列表失败: "+err.Error())
			return
		}
		commonAPI.SendPaginatedResponse(c, resp.Items, resp.Total, page, pageSize)
	}
}

// ============================================================
// 资产列表与搜索
// ============================================================

// @Summary 获取资产列表 | Get asset list
// @Tags Portal
// @Produce json
// @Param keyword query string false "搜索关键词 | Search keyword"
// @Param type_id query int false "类型ID | Type ID"
// @Param catalog_id query int false "目录ID | Catalog ID"
// @Success 200 {object} map[string]interface{}
// @Router /assets [get]
// @Security BearerAuth
// handleAssets GET /api/portal/assets
// 同时承担搜索功能（带 keyword 时走 Meilisearch）
func handleAssets(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		page, pageSize := commonAPI.GetPaginationParams(c)

		opts := commonClient.AssetQueryOptions{
			Status:   commonClient.AssetStatusPublished,
			Keyword:  c.Query("keyword"),
			Page:     page,
			PageSize: pageSize,
		}
		if typeIDStr := c.Query("type_id"); typeIDStr != "" {
			if tid, err := strconv.ParseInt(typeIDStr, 10, 64); err == nil {
				opts.TypeID = tid
			}
		}
		if catStr := c.Query("catalog_id"); catStr != "" {
			if cid, err := strconv.ParseInt(catStr, 10, 64); err == nil {
				opts.CatalogID = &cid
			}
		}

		resp, err := assetClient.GetAssets(tenantID, opts)
		if err != nil {
			commonAPI.InternalServerError(c, "获取资产列表失败: "+err.Error())
			return
		}
		commonAPI.SendPaginatedResponse(c, resp.Items, resp.Total, page, pageSize)
	}
}

// @Summary 搜索资产 | Search assets
// @Tags Portal
// @Produce json
// @Param keyword query string false "搜索关键词 | Search keyword"
// @Success 200 {object} map[string]interface{}
// @Router /search [get]
// @Security BearerAuth
// handleSearch GET /api/portal/search — 语义别名，与 handleAssets 行为相同
func handleSearch(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return handleAssets(assetClient)
}

// ============================================================
// 资产详情
// ============================================================

// ============================================================
// 资产申请（Phase 4）
// ============================================================

// @Summary 申请使用资产 | Apply for asset access
// @Tags Portal
// @Accept json
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Param body body map[string]interface{} true "申请信息 | Application info"
// @Success 201 {object} map[string]interface{}
// @Router /assets/{id}/apply [post]
// @Security BearerAuth
// handleApply POST /api/portal/assets/:id/apply
// 消费者提交资产使用申请
func handleApply(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		userID := int64(commonAuth.GetUserID(c))

		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}

		var body struct {
			Reason      string `json:"reason" binding:"required"`
			DurationDay int    `json:"duration_day"`
		}
		if !commonAPI.BindJSON(c, &body) {
			return
		}

		durationDay := body.DurationDay
		if durationDay <= 0 {
			durationDay = 30
		}

		app, err := assetClient.CreateApplication(tenantID, commonClient.CreateApplicationRequest{
			AssetID:     assetID,
			ApplicantID: userID,
			Reason:      body.Reason,
			DurationDay: durationDay,
		})
		if err != nil {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		commonAPI.CreatedResponse(c, app)
	}
}

// @Summary 获取我的申请列表 | Get my applications
// @Tags Portal
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /my/applications [get]
// @Security BearerAuth
// handleMyApplications GET /api/portal/my/applications
// 返回当前登录用户的申请列表
func handleMyApplications(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		userID := int64(commonAuth.GetUserID(c))

		apps, err := assetClient.GetApplications(tenantID, userID, 0)
		if err != nil {
			commonAPI.InternalServerError(c, "获取申请列表失败: "+err.Error())
			return
		}
		commonAPI.SuccessResponse(c, apps)
	}
}

// @Summary 获取资产申请状态 | Get asset apply status
// @Tags Portal
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Router /assets/{id}/apply-status [get]
// @Security BearerAuth
// handleApplyStatus GET /api/portal/assets/:id/apply-status
// 返回当前用户对该资产的申请/授权状态：none | pending | approved
func handleApplyStatus(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		userID := int64(commonAuth.GetUserID(c))

		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}

		status, err := assetClient.GetApplyStatus(tenantID, assetID, userID)
		if err != nil {
			commonAPI.InternalServerError(c, "查询状态失败: "+err.Error())
			return
		}
		commonAPI.SuccessResponse(c, gin.H{"status": status})
	}
}
// @Summary 获取资产详情 | Get asset detail
// @Tags Portal
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Router /assets/{id} [get]
// @Security BearerAuth
// 仅返回 published 状态的资产，否则 404
func handleAssetDetail(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}

		detail, err := assetClient.GetAssetDetail(assetID, tenantID)
		if err != nil {
			commonAPI.NotFoundError(c, "资产不存在")
			return
		}
		if detail.Status != commonClient.AssetStatusPublished {
			commonAPI.NotFoundError(c, "资产不存在")
			return
		}

		commonAPI.SuccessResponse(c, detail)
	}
}

// @Summary 获取资产服务端点 | Get asset service endpoints
// @Tags Portal
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Router /assets/{id}/endpoints [get]
// @Security BearerAuth
// handleAssetEndpoints GET /api/portal/assets/:id/endpoints
// 已授权用户获取数据服务资产的服务端点信息
func handleAssetEndpoints(assetClient *commonClient.AssetClient, serviceClient *commonClient.ServiceClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		userID := int64(commonAuth.GetUserID(c))

		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}

		// 验证用户已授权
		status, err := assetClient.GetApplyStatus(tenantID, assetID, userID)
		if err != nil || status != "approved" {
			c.JSON(403, gin.H{"error": "无访问权限，请先申请并获得授权"})
			return
		}

		// 获取资产详情（含 source_reference）
		detail, err := assetClient.GetAssetDetail(assetID, tenantID)
		if err != nil {
			commonAPI.NotFoundError(c, "资产不存在")
			return
		}

		// 仅数据服务类型有 endpoint
		if detail.SourceModule != "service" || detail.SourceReference == "" {
			commonAPI.SuccessResponse(c, gin.H{"service_type": "", "title": detail.Name, "endpoints": map[string]string{}})
			return
		}

		info, err := serviceClient.GetEndpointsByRef(tenantID, detail.SourceReference)
		if err != nil {
			commonAPI.InternalServerError(c, "获取服务端点失败: "+err.Error())
			return
		}
		commonAPI.SuccessResponse(c, info)
	}
}

// ============================================================
// 资产评价（Phase 6）
// ============================================================

// @Summary 获取资产评价列表 | Get asset ratings
// @Tags Portal
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Router /assets/{id}/ratings [get]
// @Security BearerAuth
// handleGetRatings GET /api/portal/assets/:id/ratings
// 返回评价列表 + 当前用户的评价 + 平均分统计
func handleGetRatings(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		userID := int64(commonAuth.GetUserID(c))

		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}

		ratings, total, err := assetClient.GetRatings(tenantID, assetID)
		if err != nil {
			commonAPI.InternalServerError(c, "获取评价失败: "+err.Error())
			return
		}

		// 从列表中找当前用户的评价
		var myRating *commonClient.RatingItem
		var totalScore float64
		for i := range ratings {
			totalScore += float64(ratings[i].Score)
			if ratings[i].UserID == userID {
				r := ratings[i]
				myRating = &r
			}
		}

		var avgScore float64
		if total > 0 {
			avgScore = totalScore / float64(total)
		}

		commonAPI.SuccessResponse(c, gin.H{
			"ratings":   ratings,
			"total":     total,
			"avg_score": avgScore,
			"my_rating": myRating,
		})
	}
}

// @Summary 提交资产评价 | Submit asset rating
// @Tags Portal
// @Accept json
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Param body body map[string]interface{} true "评价信息 | Rating info"
// @Success 200 {object} map[string]interface{}
// @Router /assets/{id}/ratings [post]
// @Security BearerAuth
// handleSubmitRating POST /api/portal/assets/:id/ratings
// 提交或修改评价（upsert 语义，每用户每资产只能有一条）
func handleSubmitRating(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := int64(commonAuth.GetTenantID(c))
		userID := int64(commonAuth.GetUserID(c))

		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}

		var body struct {
			Score   float32  `json:"score" binding:"required"`
			Comment string   `json:"comment"`
			Tags    []string `json:"tags"`
		}
		if !commonAPI.BindJSON(c, &body) {
			return
		}

		rating, err := assetClient.UpsertRating(tenantID, commonClient.UpsertRatingRequest{
			AssetID: assetID,
			UserID:  userID,
			Score:   body.Score,
			Comment: body.Comment,
			Tags:    body.Tags,
		})
		if err != nil {
			commonAPI.InternalServerError(c, "提交评价失败: "+err.Error())
			return
		}
		commonAPI.SuccessResponse(c, rating)
	}
}

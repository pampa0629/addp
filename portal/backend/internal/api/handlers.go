package api

import (
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

func userAccessToken(c *gin.Context) string {
	return strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
}

func writeAssetClientError(c *gin.Context, err error, message string) {
	if status, ok := commonClient.AssetAPIStatusCode(err); ok && status >= http.StatusBadRequest && status < http.StatusInternalServerError {
		commonAPI.ErrorResponse(c, status, message)
		return
	}
	commonAPI.ErrorResponse(c, http.StatusBadGateway, message)
}

// ============================================================
// 门户首页
// ============================================================

// @Summary 获取门户首页数据 | Get portal home data
// @Tags Portal
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /home [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.entry.read"]
// handleHome GET /api/portal/home
// 返回最新上架资产（前 6 条）+ 各类型统计数
func handleHome(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		accessToken := userAccessToken(c)
		ctx := c.Request.Context()

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
			resp, err := assetClient.GetAssets(ctx, accessToken, commonClient.AssetQueryOptions{
				Page:     page,
				PageSize: pageSize,
			})
			latestCh <- latestResult{resp, err}
		}()

		go func() {
			resp, err := assetClient.GetAssetStats(ctx, accessToken)
			statsCh <- statsResult{resp, err}
		}()

		lr := <-latestCh
		sr := <-statsCh

		if lr.err != nil {
			writeAssetClientError(c, lr.err, "获取资产列表失败")
			return
		}
		if sr.err != nil {
			writeAssetClientError(c, sr.err, "获取统计数据失败")
			return
		}

		commonAPI.SuccessResponse(c, gin.H{
			"latest_assets":   lr.resp.Items,
			"type_stats":      sr.resp.TypeStats,
			"total_published": sr.resp.Total,
		})
	}
}

// ============================================================
// 资产分类浏览
// ============================================================

// @Summary 获取资产分类树 | Get asset category tree
// @Description 仅包含有已上架资产的分支，count 为当前节点整棵子树的已上架资产数 | Contains only branches with published assets; count is the published asset total for the entire subtree
// @Tags Portal
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /categories [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.category.read"]
// handleCategories GET /api/v1/portal/categories
// 返回资产分类树（只含有 published 资产的分支，count 为子树已上架资产数）
func handleCategories(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		categories, err := assetClient.GetCategories(c.Request.Context(), userAccessToken(c))
		if err != nil {
			writeAssetClientError(c, err, "获取资产分类失败")
			return
		}
		commonAPI.SuccessResponse(c, categories)
	}
}

// @Summary 获取资产目录子树的资产列表 | Get assets in an asset directory subtree
// @Tags Portal
// @Produce json
// @Param id path int true "资产目录节点 ID | Asset directory node ID"
// @Success 200 {object} map[string]interface{}
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /categories/{id}/assets [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.category.read","asset.entry.read"]
// handleCategoryAssets GET /api/v1/portal/categories/:id/assets，包含当前节点及全部后代节点中的已上架资产。
func handleCategoryAssets(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		categoryID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产分类 ID")
			return
		}
		page, pageSize := commonAPI.GetPaginationParams(c)

		resp, err := assetClient.GetAssets(c.Request.Context(), userAccessToken(c), commonClient.AssetQueryOptions{
			CategoryID: &categoryID,
			Page:       page,
			PageSize:   pageSize,
		})
		if err != nil {
			writeAssetClientError(c, err, "获取资产列表失败")
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
// @Param category_id query int false "资产分类 ID | Asset category ID"
// @Success 200 {object} map[string]interface{}
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /assets [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.entry.read"]
// handleAssets GET /api/portal/assets
// 同时承担搜索功能（带 keyword 时走 Meilisearch）
func handleAssets(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, pageSize := commonAPI.GetPaginationParams(c)

		opts := commonClient.AssetQueryOptions{
			Keyword:  c.Query("keyword"),
			Page:     page,
			PageSize: pageSize,
		}
		if typeIDStr := c.Query("type_id"); typeIDStr != "" {
			if tid, err := strconv.ParseInt(typeIDStr, 10, 64); err == nil {
				opts.TypeID = tid
			}
		}
		if categoryValue := c.Query("category_id"); categoryValue != "" {
			if categoryID, err := strconv.ParseInt(categoryValue, 10, 64); err == nil {
				opts.CategoryID = &categoryID
			}
		}

		resp, err := assetClient.GetAssets(c.Request.Context(), userAccessToken(c), opts)
		if err != nil {
			writeAssetClientError(c, err, "获取资产列表失败")
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
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /search [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.entry.read"]
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
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /assets/{id}/apply [post]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.application.create"]
// handleApply POST /api/portal/assets/:id/apply
// 消费者提交资产使用申请
func handleApply(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		app, err := assetClient.CreateApplication(c.Request.Context(), userAccessToken(c), assetID, commonClient.CreateApplicationRequest{
			Reason:      body.Reason,
			DurationDay: durationDay,
		})
		if err != nil {
			writeAssetClientError(c, err, "提交资产申请失败")
			return
		}
		commonAPI.CreatedResponse(c, app)
	}
}

// @Summary 获取我的申请列表 | Get my applications
// @Tags Portal
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /my/applications [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.application.read"]
// handleMyApplications GET /api/portal/my/applications
// 返回当前登录用户的申请列表
func handleMyApplications(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		apps, err := assetClient.GetApplications(c.Request.Context(), userAccessToken(c))
		if err != nil {
			writeAssetClientError(c, err, "获取申请列表失败")
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
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /assets/{id}/apply-status [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.application.read","asset.authorization.read"]
// handleApplyStatus GET /api/portal/assets/:id/apply-status
// 返回当前用户对该资产的申请/履约状态以及可选消费入口。
func handleApplyStatus(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}

		status, err := assetClient.GetApplyStatus(c.Request.Context(), userAccessToken(c), assetID)
		if err != nil {
			writeAssetClientError(c, err, "查询资产申请状态失败")
			return
		}
		commonAPI.SuccessResponse(c, status)
	}
}

// @Summary 获取资产详情 | Get asset detail
// @Tags Portal
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /assets/{id} [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.entry.read"]
// 仅返回 published 状态的资产，否则 404
func handleAssetDetail(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}

		detail, err := assetClient.GetAssetDetail(c.Request.Context(), userAccessToken(c), assetID)
		if err != nil {
			writeAssetClientError(c, err, "获取资产详情失败")
			return
		}
		if detail.Status != commonClient.AssetStatusPublished {
			commonAPI.NotFoundError(c, "资产不存在")
			return
		}

		commonAPI.SuccessResponse(c, detail)
	}
}

// ============================================================
// 资产评价（Phase 6）
// ============================================================

type portalRatingsResponse struct {
	Ratings  []commonClient.RatingItem `json:"ratings"`
	Total    int64                     `json:"total"`
	AvgScore float64                   `json:"avg_score"`
	MyRating *commonClient.RatingItem  `json:"my_rating"`
}

// @Summary 获取资产评价列表 | Get asset ratings
// @Description 当前用户评价与全量平均分来自 Asset，不依赖公开列表的当前分页 | Current user's rating and overall average come from Asset independently of the public page
// @Tags Portal
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Success 200 {object} portalRatingsResponse
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /assets/{id}/ratings [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.rating.read"]
// handleGetRatings GET /api/portal/assets/:id/ratings
// 返回评价列表 + 当前用户的评价 + 平均分统计
func handleGetRatings(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}

		ratings, total, myRating, avgScore, err := assetClient.GetRatings(c.Request.Context(), userAccessToken(c), assetID)
		if err != nil {
			writeAssetClientError(c, err, "获取评价失败")
			return
		}

		commonAPI.SuccessResponse(c, portalRatingsResponse{
			Ratings: ratings, Total: total, AvgScore: avgScore, MyRating: myRating,
		})
	}
}

type portalOwnRatingResponse struct {
	Rating *commonClient.RatingItem `json:"rating"`
}

// @Summary 获取本人资产评价以修改 | Get own asset rating for editing
// @Tags Portal
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Success 200 {object} portalOwnRatingResponse
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /assets/{id}/my-rating [get]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.rating.update"]
func handleGetOwnRatingForUpdate(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			commonAPI.BadRequestError(c, "无效的资产 ID")
			return
		}
		rating, err := assetClient.GetOwnRatingForUpdate(c.Request.Context(), userAccessToken(c), assetID)
		if err != nil {
			writeAssetClientError(c, err, "获取评价失败")
			return
		}
		commonAPI.SuccessResponse(c, portalOwnRatingResponse{Rating: rating})
	}
}

// @Summary 创建资产评价 | Create asset rating
// @Tags Portal
// @Accept json
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Param body body map[string]interface{} true "评价信息 | Rating info"
// @Success 201 {object} map[string]interface{}
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /assets/{id}/ratings [post]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.rating.create"]
func handleCreateRating(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return handleWriteRating(assetClient, true)
}

// @Summary 修改本人资产评价 | Update own asset rating
// @Tags Portal
// @Accept json
// @Produce json
// @Param id path int true "资产ID | Asset ID"
// @Param body body map[string]interface{} true "评价信息 | Rating info"
// @Success 200 {object} map[string]interface{}
// @Failure 502 {object} map[string]string "资产服务调用失败 | Asset service request failed"
// @Router /assets/{id}/ratings [put]
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.rating.update"]
func handleUpdateRating(assetClient *commonClient.AssetClient) gin.HandlerFunc {
	return handleWriteRating(assetClient, false)
}

func handleWriteRating(assetClient *commonClient.AssetClient, create bool) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		request := commonClient.RatingWriteRequest{
			Score:   body.Score,
			Comment: body.Comment,
			Tags:    body.Tags,
		}
		var rating *commonClient.RatingItem
		if create {
			rating, err = assetClient.CreateRating(c.Request.Context(), userAccessToken(c), assetID, request)
		} else {
			rating, err = assetClient.UpdateRating(c.Request.Context(), userAccessToken(c), assetID, request)
		}
		if err != nil {
			writeAssetClientError(c, err, "提交评价失败")
			return
		}
		if create {
			commonAPI.CreatedResponse(c, rating)
		} else {
			commonAPI.SuccessResponse(c, rating)
		}
	}
}

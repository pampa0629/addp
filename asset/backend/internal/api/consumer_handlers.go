package api

import (
	"strconv"

	i18nkeys "github.com/addp/asset/i18n"
	"github.com/addp/asset/internal/service"
	commonAPI "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

type consumerApplicationRequest struct {
	Reason      string `json:"reason" binding:"required"`
	DurationDay int    `json:"duration_day"`
}

type consumerRatingRequest struct {
	Score   float32  `json:"score" binding:"required,min=1,max=5"`
	Comment string   `json:"comment"`
	Tags    []string `json:"tags"`
}

// listConsumerAssets godoc
// @Summary 浏览已上架资产 | Browse published assets
// @Tags Asset Consumer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.entry.read"]
// @Router /consumer/assets [get]
func (h *Handler) listConsumerAssets(c *gin.Context) {
	page, pageSize := commonAPI.GetPaginationParams(c)
	typeID, _ := strconv.ParseInt(c.Query("type_id"), 10, 64)
	params := &service.AssetListParams{
		Page: page, PageSize: pageSize, Status: "published", TypeID: typeID, Keyword: c.Query("keyword"),
	}
	if value := c.Query("category_id"); value != "" {
		if categoryID, err := strconv.ParseInt(value, 10, 64); err == nil {
			params.CategoryID = &categoryID
		}
	}
	assets, total, err := h.assetSvc.List(commonAuth.GetTenantID(c), params)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SendPaginatedResponse(c, assets, total, page, pageSize)
}

// getConsumerAsset godoc
// @Summary 获取已上架资产详情 | Get published asset detail
// @Tags Asset Consumer
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.entry.read"]
// @Router /consumer/assets/{id} [get]
func (h *Handler) getConsumerAsset(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	asset, err := h.assetSvc.GetPublished(commonAuth.GetTenantID(c), id)
	if err != nil {
		commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgAssetNotFound))
		return
	}
	commonAPI.SuccessResponse(c, asset)
}

// getConsumerAssetStats godoc
// @Summary 获取已上架资产统计 | Get published asset statistics
// @Tags Asset Consumer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.entry.read"]
// @Router /consumer/assets/stats [get]
func (h *Handler) getConsumerAssetStats(c *gin.Context) {
	stats, err := h.assetSvc.GetStats(commonAuth.GetTenantID(c))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, stats)
}

// listConsumerCategories godoc
// @Summary 浏览已上架资产分类 | Browse published asset categories
// @Tags Asset Consumer
// @Produce json
// @Success 200 {array} service.AssetCategoryTreeNode
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.category.read"]
// @Router /consumer/categories [get]
func (h *Handler) listConsumerCategories(c *gin.Context) {
	categories, err := h.categorySvc.GetPublishedTree(commonAuth.GetTenantID(c))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, categories)
}

// createConsumerApplication godoc
// @Summary 申请使用资产 | Apply for asset access
// @Tags Asset Consumer
// @Accept json
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Param request body consumerApplicationRequest true "申请 | Application"
// @Success 201 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.application.create"]
// @Router /consumer/assets/{id}/applications [post]
func (h *Handler) createConsumerApplication(c *gin.Context) {
	assetID, ok := pathID(c, "id")
	if !ok {
		return
	}
	var request consumerApplicationRequest
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	application, err := h.applicationSvc.Create(
		commonAuth.GetTenantID(c),
		int64(commonAuth.GetUserID(c)),
		&service.CreateApplicationReq{AssetID: assetID, Reason: request.Reason, DurationDay: request.DurationDay},
	)
	if err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.CreatedResponse(c, application)
}

// listConsumerApplications godoc
// @Summary 获取我的资产申请 | List my asset applications
// @Tags Asset Consumer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.application.read"]
// @Router /consumer/applications [get]
func (h *Handler) listConsumerApplications(c *gin.Context) {
	page, pageSize := commonAPI.GetPaginationParams(c)
	assetID, _ := strconv.ParseInt(c.Query("asset_id"), 10, 64)
	params := service.ApplicationListParams{
		Page: page, PageSize: pageSize, Status: c.Query("status"), DisplayStatus: c.Query("display_status"),
		AssetID: assetID, ApplicantID: int64(commonAuth.GetUserID(c)),
	}
	applications, total, err := h.applicationSvc.List(commonAuth.GetTenantID(c), params)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SendPaginatedResponse(c, applications, total, page, pageSize)
}

// getConsumerApplicationStatus godoc
// @Summary 获取我的资产申请状态 | Get my asset application status
// @Tags Asset Consumer
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Success 200 {object} service.ConsumerAccessStatus
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.application.read","asset.authorization.read"]
// @Router /consumer/assets/{id}/application-status [get]
func (h *Handler) getConsumerApplicationStatus(c *gin.Context) {
	assetID, ok := pathID(c, "id")
	if !ok {
		return
	}
	status, err := h.applicationSvc.ConsumerStatus(
		commonAuth.GetTenantID(c), int64(commonAuth.GetUserID(c)), assetID,
	)
	if err != nil {
		commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgAssetNotFound))
		return
	}
	commonAPI.SuccessResponse(c, status)
}

// listConsumerRatings godoc
// @Summary 获取已上架资产评价 | List published asset ratings
// @Tags Asset Consumer
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.rating.read"]
// @Router /consumer/assets/{id}/ratings [get]
func (h *Handler) listConsumerRatings(c *gin.Context) {
	assetID, ok := pathID(c, "id")
	if !ok {
		return
	}
	if _, err := h.assetSvc.GetPublished(commonAuth.GetTenantID(c), assetID); err != nil {
		commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgAssetNotFound))
		return
	}
	page, pageSize := commonAPI.GetPaginationParams(c)
	ratings, total, err := h.ratingSvc.List(commonAuth.GetTenantID(c), service.RatingListParams{
		AssetID: assetID, Page: page, PageSize: pageSize,
	})
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SendPaginatedResponse(c, ratings, total, page, pageSize)
}

// upsertConsumerRating godoc
// @Summary 提交或更新资产评价 | Submit or update asset rating
// @Tags Asset Consumer
// @Accept json
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Param request body consumerRatingRequest true "评价 | Rating"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.rating.create","asset.rating.update"]
// @Router /consumer/assets/{id}/ratings [post]
func (h *Handler) upsertConsumerRating(c *gin.Context) {
	assetID, ok := pathID(c, "id")
	if !ok {
		return
	}
	if _, err := h.assetSvc.GetPublished(commonAuth.GetTenantID(c), assetID); err != nil {
		commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgAssetNotFound))
		return
	}
	var request consumerRatingRequest
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	rating, err := h.ratingSvc.Upsert(
		commonAuth.GetTenantID(c), int64(commonAuth.GetUserID(c)), assetID,
		&service.UpsertRatingReq{Score: request.Score, Comment: request.Comment, Tags: request.Tags},
	)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, rating)
}

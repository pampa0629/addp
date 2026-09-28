package api

import (
	"errors"
	"net/http"
	"strconv"

	i18nkeys "github.com/addp/asset/i18n"
	"github.com/addp/asset/internal/models"
	"github.com/addp/asset/internal/service"
	commonAPI "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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

type consumerRatingsResponse struct {
	Data       []service.RatingWithUser `json:"data"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"page_size"`
	TotalPages int                      `json:"total_pages"`
	MyRating   *models.Rating           `json:"my_rating"`
	AvgScore   float64                  `json:"avg_score"`
}

type ownConsumerRatingResponse struct {
	Rating *models.Rating `json:"rating"`
}

// listConsumerAssets godoc
// @Summary 浏览已上架资产 | Browse published assets
// @Tags Asset Consumer
// @Produce json
// @Param category_id query int false "资产目录节点 ID；返回该节点及全部后代节点中的已上架资产 | Asset directory node ID; returns published assets in the node subtree"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string "无效的资产目录节点 ID | Invalid asset directory node ID"
// @Failure 404 {object} map[string]string "资产目录节点不存在 | Asset directory node not found"
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
		categoryID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || categoryID <= 0 {
			commonAPI.BadRequestError(c, commoni18n.T(c, i18nkeys.MsgInvalidID))
			return
		}
		categoryIDs, err := h.categorySvc.SubtreeIDs(commonAuth.GetTenantID(c), categoryID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgCategoryNotFound))
			return
		}
		if err != nil {
			commonAPI.InternalServerError(c, err.Error())
			return
		}
		params.CategoryIDs = categoryIDs
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
// @Description 仅返回包含已上架资产的目录分支，count 为当前节点整棵子树的已上架资产数 | Returns only branches containing published assets; count is the published asset total for the entire subtree
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
// @Description 分页返回公开评价，并独立返回当前用户评价与全量平均分 | Paginate published ratings and independently return the current user's rating and overall average
// @Tags Asset Consumer
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Success 200 {object} consumerRatingsResponse
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
	myRating, err := h.ratingSvc.GetByUser(commonAuth.GetTenantID(c), int64(commonAuth.GetUserID(c)), assetID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	average, err := h.ratingSvc.Average(commonAuth.GetTenantID(c), assetID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	commonAPI.SuccessResponse(c, consumerRatingsResponse{
		Data: ratings, Total: total, Page: page, PageSize: pageSize,
		TotalPages: totalPages, MyRating: myRating, AvgScore: average,
	})
}

// getOwnConsumerRatingForUpdate godoc
// @Summary 获取本人评价以修改 | Get own rating for editing
// @Description 仅返回当前用户在已上架资产上的评价，不开放其他用户评价列表 | Return only the current user's rating on a published asset, without listing other users' ratings
// @Tags Asset Consumer
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Success 200 {object} ownConsumerRatingResponse
// @Failure 404 {object} map[string]string "资产不存在 | Asset not found"
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.rating.update"]
// @Router /consumer/assets/{id}/my-rating [get]
func (h *Handler) getOwnConsumerRatingForUpdate(c *gin.Context) {
	assetID, ok := pathID(c, "id")
	if !ok {
		return
	}
	if _, err := h.assetSvc.GetPublished(commonAuth.GetTenantID(c), assetID); err != nil {
		commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgAssetNotFound))
		return
	}
	rating, err := h.ratingSvc.GetByUser(commonAuth.GetTenantID(c), int64(commonAuth.GetUserID(c)), assetID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, ownConsumerRatingResponse{Rating: rating})
}

// createConsumerRating godoc
// @Summary 创建资产评价 | Create asset rating
// @Tags Asset Consumer
// @Accept json
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Param request body consumerRatingRequest true "评价 | Rating"
// @Success 201 {object} map[string]interface{}
// @Failure 403 {object} map[string]string "没有有效资产授权 | No effective asset authorization"
// @Failure 409 {object} map[string]string "本人评价已存在 | Own rating already exists"
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.rating.create"]
// @Router /consumer/assets/{id}/ratings [post]
func (h *Handler) createConsumerRating(c *gin.Context) {
	h.writeConsumerRating(c, true)
}

// updateConsumerRating godoc
// @Summary 修改本人资产评价 | Update own asset rating
// @Tags Asset Consumer
// @Accept json
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Param request body consumerRatingRequest true "评价 | Rating"
// @Success 200 {object} map[string]interface{}
// @Failure 403 {object} map[string]string "没有有效资产授权 | No effective asset authorization"
// @Failure 404 {object} map[string]string "本人评价不存在 | Own rating not found"
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.rating.update"]
// @Router /consumer/assets/{id}/ratings [put]
func (h *Handler) updateConsumerRating(c *gin.Context) {
	h.writeConsumerRating(c, false)
}

func (h *Handler) writeConsumerRating(c *gin.Context, create bool) {
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
	tenantID, userID := commonAuth.GetTenantID(c), int64(commonAuth.GetUserID(c))
	writeRequest := &service.RatingWriteReq{Score: request.Score, Comment: request.Comment, Tags: request.Tags}
	var rating *models.Rating
	var err error
	if create {
		rating, err = h.ratingSvc.Create(tenantID, userID, assetID, writeRequest)
	} else {
		rating, err = h.ratingSvc.Update(tenantID, userID, assetID, writeRequest)
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrRatingAssetHidden):
			commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgAssetNotFound))
		case errors.Is(err, service.ErrRatingAccessDenied):
			c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, i18nkeys.MsgRatingAccessDenied), "error_code": "asset_rating_access_denied"})
		case errors.Is(err, service.ErrRatingAlreadyExists):
			c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, i18nkeys.MsgRatingAlreadyExists), "error_code": "asset_rating_already_exists"})
		case errors.Is(err, service.ErrRatingNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, i18nkeys.MsgRatingNotFound), "error_code": "asset_rating_not_found"})
		default:
			commonAPI.InternalServerError(c, err.Error())
		}
		return
	}
	if create {
		commonAPI.CreatedResponse(c, rating)
	} else {
		commonAPI.SuccessResponse(c, rating)
	}
}

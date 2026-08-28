package api

import (
	"errors"
	"net/http"
	"strconv"

	i18nkeys "github.com/addp/asset/i18n"
	"github.com/addp/asset/internal/service"
	commonAPI "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	typeSvc          *service.TypeService
	categorySvc      *service.CategoryService
	assetSvc         *service.AssetService
	applicationSvc   *service.ApplicationService
	authorizationSvc *service.AuthorizationService
	ratingSvc        *service.RatingService
}

type ratingHandledRequest struct {
	IsHandled bool `json:"is_handled"`
}

func newHandler(db *gorm.DB, assetSvc *service.AssetService) *Handler {
	authorizationSvc := service.NewAuthorizationService(db)
	return &Handler{
		typeSvc:          service.NewTypeService(db),
		categorySvc:      service.NewCategoryService(db),
		assetSvc:         assetSvc,
		applicationSvc:   service.NewApplicationService(db, authorizationSvc),
		authorizationSvc: authorizationSvc,
		ratingSvc:        service.NewRatingService(db),
	}
}

func pathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil {
		commonAPI.BadRequestError(c, commoni18n.T(c, i18nkeys.MsgInvalidID))
		return 0, false
	}
	return id, true
}

// listTypes godoc
// @Summary 获取资产类型 | List asset types
// @Tags Asset Type
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.read"]
// @Router /type-definitions [get]
func (h *Handler) listTypes(c *gin.Context) {
	types, err := h.typeSvc.ListTypes(commonAuth.GetTenantID(c))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, types)
}

// getType godoc
// @Summary 获取资产类型详情 | Get asset type
// @Tags Asset Type
// @Produce json
// @Param id path int true "类型 ID | Type ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.read"]
// @Router /type-definitions/{id} [get]
func (h *Handler) getType(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	typeDefinition, err := h.typeSvc.GetType(id)
	if err != nil {
		commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgTypeNotFound))
		return
	}
	commonAPI.SuccessResponse(c, typeDefinition)
}

// listCategories godoc
// @Summary 获取资产分类 | List asset categories
// @Tags Asset Category
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.category.read"]
// @Router /categories [get]
func (h *Handler) listCategories(c *gin.Context) {
	categories, err := h.categorySvc.ListAll(commonAuth.GetTenantID(c))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, categories)
}

// getCategoryTree godoc
// @Summary 获取资产分类树 | Get asset category tree
// @Tags Asset Category
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.category.read"]
// @Router /categories/tree [get]
func (h *Handler) getCategoryTree(c *gin.Context) {
	tree, err := h.categorySvc.GetTree(commonAuth.GetTenantID(c))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, tree)
}

// getCategory godoc
// @Summary 获取资产分类详情 | Get asset category
// @Tags Asset Category
// @Produce json
// @Param id path int true "资产分类 ID | Asset category ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @Failure 404 {object} map[string]string
// @x-addp-required-permissions ["asset.management.read","asset.category.read"]
// @Router /categories/{id} [get]
func (h *Handler) getCategory(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	category, err := h.categorySvc.Get(commonAuth.GetTenantID(c), id)
	if err != nil {
		commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgCategoryNotFound))
		return
	}
	commonAPI.SuccessResponse(c, category)
}

// createCategory godoc
// @Summary 创建资产分类 | Create asset category
// @Tags Asset Category
// @Accept json
// @Produce json
// @Param request body service.CreateAssetCategoryRequest true "资产分类 | Asset category"
// @Success 201 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.category.create"]
// @Router /categories [post]
func (h *Handler) createCategory(c *gin.Context) {
	var request service.CreateAssetCategoryRequest
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	category, err := h.categorySvc.Create(commonAuth.GetTenantID(c), &request)
	if err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.CreatedResponse(c, category)
}

// updateCategory godoc
// @Summary 更新资产分类 | Update asset category
// @Tags Asset Category
// @Accept json
// @Produce json
// @Param id path int true "资产分类 ID | Asset category ID"
// @Param request body service.UpdateAssetCategoryRequest true "资产分类 | Asset category"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.category.update"]
// @Router /categories/{id} [put]
func (h *Handler) updateCategory(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var request service.UpdateAssetCategoryRequest
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	category, err := h.categorySvc.Update(commonAuth.GetTenantID(c), id, &request)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgCategoryNotFound))
			return
		}
		if errors.Is(err, service.ErrAssetCategoryVersionConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, i18nkeys.MsgCategoryVersionConflict), "error_code": "asset_category_version_conflict"})
			return
		}
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, category)
}

// deleteCategory godoc
// @Summary 删除资产分类 | Delete asset category
// @Tags Asset Category
// @Accept json
// @Produce json
// @Param id path int true "资产分类 ID | Asset category ID"
// @Param request body service.DeleteAssetCategoryRequest true "并发版本 | Concurrency version"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.category.delete"]
// @Router /categories/{id} [delete]
func (h *Handler) deleteCategory(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var request service.DeleteAssetCategoryRequest
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	if err := h.categorySvc.Delete(commonAuth.GetTenantID(c), id, request.Version); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgCategoryNotFound))
			return
		}
		if errors.Is(err, service.ErrAssetCategoryVersionConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, i18nkeys.MsgCategoryVersionConflict), "error_code": "asset_category_version_conflict"})
			return
		}
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"message": commoni18n.T(c, i18nkeys.MsgDeleteSuccess)})
}

// listAssets godoc
// @Summary 获取资产列表 | List assets
// @Tags Asset
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.read"]
// @Router /assets [get]
func (h *Handler) listAssets(c *gin.Context) {
	page, pageSize := commonAPI.GetPaginationParams(c)
	typeID, _ := strconv.ParseInt(c.Query("type_id"), 10, 64)
	params := &service.AssetListParams{
		Page: page, PageSize: pageSize, Status: c.Query("status"), TypeID: typeID, Keyword: c.Query("keyword"),
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

// getAsset godoc
// @Summary 获取资产详情 | Get asset
// @Tags Asset
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.read"]
// @Router /assets/{id} [get]
func (h *Handler) getAsset(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	asset, err := h.assetSvc.Get(commonAuth.GetTenantID(c), id)
	if err != nil {
		commonAPI.NotFoundError(c, commoni18n.T(c, i18nkeys.MsgAssetNotFound))
		return
	}
	commonAPI.SuccessResponse(c, asset)
}

// createAsset godoc
// @Summary 创建资产 | Create asset
// @Description 选择一个或多个 CatalogEntry 原子创建资产草稿 | Atomically create an asset draft by selecting one or more CatalogEntry references
// @Tags Asset
// @Accept json
// @Produce json
// @Param request body service.CreateAssetReq true "完整资产聚合 | Complete asset aggregate"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.update"]
// @Router /assets [post]
func (h *Handler) createAsset(c *gin.Context) {
	var request service.CreateAssetReq
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	asset, err := h.assetSvc.Create(c.Request.Context(), commonAuth.GetTenantID(c), commonAuth.GetUserID(c), &request)
	if err != nil {
		respondAssetOperationError(c, err)
		return
	}
	commonAPI.CreatedResponse(c, asset)
}

// updateAsset godoc
// @Summary 更新资产 | Update asset
// @Tags Asset
// @Accept json
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Param request body service.UpdateAssetReq true "资产 | Asset"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.update"]
// @Router /assets/{id} [put]
func (h *Handler) updateAsset(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var request service.UpdateAssetReq
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	asset, err := h.assetSvc.Update(c.Request.Context(), commonAuth.GetTenantID(c), id, commonAuth.GetUserID(c), &request)
	if err != nil {
		respondAssetOperationError(c, err)
		return
	}
	commonAPI.SuccessResponse(c, asset)
}

// deleteAsset godoc
// @Summary 删除草稿或已下架资产 | Delete draft or offline asset
// @Description 已发布资产必须先下架再删除 | Published assets must be offlined before deletion
// @Tags Asset
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.delete"]
// @Router /assets/{id} [delete]
func (h *Handler) deleteAsset(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.assetSvc.Delete(commonAuth.GetTenantID(c), id); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"message": commoni18n.T(c, i18nkeys.MsgDeleteSuccess)})
}

// publishAsset godoc
// @Summary 上架资产 | Publish asset
// @Tags Asset
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.publish"]
// @Router /assets/{id}/publish [post]
func (h *Handler) publishAsset(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.assetSvc.Publish(c.Request.Context(), commonAuth.GetTenantID(c), id); err != nil {
		respondAssetOperationError(c, err)
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"message": commoni18n.T(c, i18nkeys.MsgAssetPublished)})
}

// offlineAsset godoc
// @Summary 下架资产 | Offline asset
// @Tags Asset
// @Produce json
// @Param id path int true "资产 ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.offline"]
// @Router /assets/{id}/offline [post]
func (h *Handler) offlineAsset(c *gin.Context) {
	h.assetStatusAction(c, h.assetSvc.Offline, i18nkeys.MsgAssetOfflined)
}

func (h *Handler) assetStatusAction(c *gin.Context, action func(uint, int64) error, messageKey string) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := action(commonAuth.GetTenantID(c), id); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"message": commoni18n.T(c, messageKey)})
}

// batchPublishAssets godoc
// @Summary 批量上架资产 | Batch publish assets
// @Tags Asset
// @Accept json
// @Produce json
// @Param request body service.BatchIDsReq true "资产 ID | Asset IDs"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.publish"]
// @Router /assets/batch-publish [post]
func (h *Handler) batchPublishAssets(c *gin.Context) {
	var request service.BatchIDsReq
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	count, err := h.assetSvc.BatchPublish(c.Request.Context(), commonAuth.GetTenantID(c), request.IDs)
	if err != nil {
		respondAssetOperationError(c, err)
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"affected": count})
}

// batchOfflineAssets godoc
// @Summary 批量下架资产 | Batch offline assets
// @Tags Asset
// @Accept json
// @Produce json
// @Param request body service.BatchIDsReq true "资产 ID | Asset IDs"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.offline"]
// @Router /assets/batch-offline [post]
func (h *Handler) batchOfflineAssets(c *gin.Context) {
	h.batchAssetAction(c, h.assetSvc.BatchOffline)
}

func (h *Handler) batchAssetAction(c *gin.Context, action func(uint, []int64) (int, error)) {
	var request service.BatchIDsReq
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	count, err := action(commonAuth.GetTenantID(c), request.IDs)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"affected": count})
}

// batchCategoryAssets godoc
// @Summary 批量设置资产分类 | Batch categorize assets
// @Tags Asset
// @Accept json
// @Produce json
// @Param request body service.BatchCategoryRequest true "资产分类变更 | Asset category change"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.update"]
// @Router /assets/batch-category [post]
func (h *Handler) batchCategoryAssets(c *gin.Context) {
	var request service.BatchCategoryRequest
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	count, err := h.assetSvc.BatchCategory(commonAuth.GetTenantID(c), request.IDs, request.CategoryID)
	if err != nil {
		respondAssetOperationError(c, err)
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"affected": count})
}

func respondAssetOperationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCatalogUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, i18nkeys.MsgCatalogUnavailable), "error_code": "asset_catalog_unavailable"})
	case errors.Is(err, service.ErrAssetVersionConflict):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, i18nkeys.MsgVersionConflict), "error_code": "asset_version_conflict"})
	case errors.Is(err, service.ErrAssetNotEditable):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, i18nkeys.MsgNotEditable), "error_code": "asset_not_editable"})
	case errors.Is(err, service.ErrCatalogReferenceNotSelectable):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, i18nkeys.MsgReferenceNotSelectable), "error_code": "asset_catalog_reference_not_selectable"})
	case errors.Is(err, service.ErrCatalogReferenceNotPublishable):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, i18nkeys.MsgReferenceNotPublishable), "error_code": "asset_catalog_reference_not_publishable"})
	default:
		commonAPI.BadRequestError(c, err.Error())
	}
}

// getAssetStats godoc
// @Summary 获取资产统计 | Get asset statistics
// @Tags Asset
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.read"]
// @Router /assets/stats [get]
func (h *Handler) getAssetStats(c *gin.Context) {
	result, err := h.assetSvc.GetStats(commonAuth.GetTenantID(c))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, result)
}

// getAssetDashboardStats godoc
// @Summary 获取资产运营统计 | Get asset dashboard statistics
// @Tags Asset
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.read"]
// @Router /assets/stats/dashboard [get]
func (h *Handler) getAssetDashboardStats(c *gin.Context) {
	result, err := h.assetSvc.GetDashboardStats(commonAuth.GetTenantID(c))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, result)
}

// getAssetTypeFields godoc
// @Summary 获取资产类型字段 | Get asset type fields
// @Tags Asset
// @Produce json
// @Param type_id path int true "类型 ID | Type ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.entry.read"]
// @Router /assets/type-fields/{type_id} [get]
func (h *Handler) getAssetTypeFields(c *gin.Context) {
	typeID, err := strconv.ParseInt(c.Param("type_id"), 10, 64)
	if err != nil {
		commonAPI.BadRequestError(c, commoni18n.T(c, i18nkeys.MsgInvalidTypeID))
		return
	}
	fields, err := h.assetSvc.GetTypeFieldSchemas(typeID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, fields)
}

// listApplications godoc
// @Summary 获取资产申请 | List asset applications
// @Tags Asset Application
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.application.read"]
// @Router /applications [get]
func (h *Handler) listApplications(c *gin.Context) {
	page, pageSize := commonAPI.GetPaginationParams(c)
	params := service.ApplicationListParams{
		Page: page, PageSize: pageSize, Status: c.Query("status"), DisplayStatus: c.Query("display_status"),
	}
	params.AssetID, _ = strconv.ParseInt(c.Query("asset_id"), 10, 64)
	params.ApplicantID, _ = strconv.ParseInt(c.Query("applicant_id"), 10, 64)
	items, total, err := h.applicationSvc.List(commonAuth.GetTenantID(c), params)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SendPaginatedResponse(c, items, total, page, pageSize)
}

// getApplication godoc
// @Summary 获取资产申请详情 | Get asset application
// @Tags Asset Application
// @Produce json
// @Param id path int true "申请 ID | Application ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.application.read"]
// @Router /applications/{id} [get]
func (h *Handler) getApplication(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	application, err := h.applicationSvc.Get(commonAuth.GetTenantID(c), id)
	if err != nil {
		commonAPI.NotFoundError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, application)
}

// approveApplication godoc
// @Summary 批准资产申请 | Approve asset application
// @Tags Asset Application
// @Accept json
// @Produce json
// @Param id path int true "申请 ID | Application ID"
// @Param request body service.ApproveApplicationReq true "审批 | Review"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.application.approve"]
// @Router /applications/{id}/approve [post]
func (h *Handler) approveApplication(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var request service.ApproveApplicationReq
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	if err := h.applicationSvc.Approve(commonAuth.GetTenantID(c), commonAuth.GetUserID(c), id, &request); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"message": commoni18n.T(c, i18nkeys.MsgApproveSuccess)})
}

// rejectApplication godoc
// @Summary 驳回资产申请 | Reject asset application
// @Tags Asset Application
// @Accept json
// @Produce json
// @Param id path int true "申请 ID | Application ID"
// @Param request body service.RejectApplicationReq true "审批 | Review"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.application.reject"]
// @Router /applications/{id}/reject [post]
func (h *Handler) rejectApplication(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var request service.RejectApplicationReq
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	if err := h.applicationSvc.Reject(commonAuth.GetTenantID(c), commonAuth.GetUserID(c), id, &request); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"message": commoni18n.T(c, i18nkeys.MsgRejectSuccess)})
}

// revokeApplication godoc
// @Summary 撤销申请授权 | Revoke application authorization
// @Tags Asset Application
// @Produce json
// @Param id path int true "申请 ID | Application ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.application.revoke"]
// @Router /applications/{id}/revoke [post]
func (h *Handler) revokeApplication(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.applicationSvc.RevokeByApplication(commonAuth.GetTenantID(c), commonAuth.GetUserID(c), id); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"message": commoni18n.T(c, i18nkeys.MsgRevokeSuccess)})
}

// listAuthorizations godoc
// @Summary 获取资产授权 | List asset authorizations
// @Tags Asset Authorization
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.authorization.read"]
// @Router /authorizations [get]
func (h *Handler) listAuthorizations(c *gin.Context) {
	page, pageSize := commonAPI.GetPaginationParams(c)
	params := service.AuthorizationListParams{Page: page, PageSize: pageSize}
	params.UserID, _ = strconv.ParseInt(c.Query("user_id"), 10, 64)
	params.AssetID, _ = strconv.ParseInt(c.Query("asset_id"), 10, 64)
	params.Status = c.Query("status")
	items, total, err := h.authorizationSvc.List(commonAuth.GetTenantID(c), params)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SendPaginatedResponse(c, items, total, page, pageSize)
}

// getAuthorization godoc
// @Summary 获取资产授权详情 | Get asset authorization
// @Tags Asset Authorization
// @Produce json
// @Param id path int true "授权 ID | Authorization ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.authorization.read"]
// @Router /authorizations/{id} [get]
func (h *Handler) getAuthorization(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	authorization, err := h.authorizationSvc.Get(commonAuth.GetTenantID(c), id)
	if err != nil {
		commonAPI.NotFoundError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, authorization)
}

// revokeAuthorization godoc
// @Summary 撤销资产授权 | Revoke asset authorization
// @Tags Asset Authorization
// @Produce json
// @Param id path int true "授权 ID | Authorization ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.authorization.revoke"]
// @Router /authorizations/{id}/revoke [post]
func (h *Handler) revokeAuthorization(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.authorizationSvc.Revoke(commonAuth.GetTenantID(c), commonAuth.GetUserID(c), id); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"message": commoni18n.T(c, i18nkeys.MsgRevokeSuccess)})
}

// listRatings godoc
// @Summary 获取资产评价 | List asset ratings
// @Tags Asset Rating
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.rating.read"]
// @Router /ratings [get]
func (h *Handler) listRatings(c *gin.Context) {
	page, pageSize := commonAPI.GetPaginationParams(c)
	params := service.RatingListParams{Page: page, PageSize: pageSize}
	params.AssetID, _ = strconv.ParseInt(c.Query("asset_id"), 10, 64)
	params.UserID, _ = strconv.ParseInt(c.Query("user_id"), 10, 64)
	params.HasFeedback = c.Query("has_feedback") == "true"
	if value := c.Query("is_handled"); value == "true" || value == "false" {
		handled := value == "true"
		params.IsHandled = &handled
	}
	items, total, err := h.ratingSvc.List(commonAuth.GetTenantID(c), params)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SendPaginatedResponse(c, items, total, page, pageSize)
}

// markRatingHandled godoc
// @Summary 标记评价反馈状态 | Mark rating feedback status
// @Tags Asset Rating
// @Accept json
// @Produce json
// @Param id path int true "评价 ID | Rating ID"
// @Param request body ratingHandledRequest true "处理状态 | Handled status"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.rating.update"]
// @Router /ratings/{id}/mark-handled [post]
func (h *Handler) markRatingHandled(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var request ratingHandledRequest
	if !commonAPI.BindJSON(c, &request) {
		return
	}
	if err := h.ratingSvc.MarkHandled(commonAuth.GetTenantID(c), id, request.IsHandled); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, gin.H{"message": commoni18n.T(c, i18nkeys.MsgUpdateSuccess)})
}

// getRatingStats godoc
// @Summary 获取资产评价统计 | Get asset rating statistics
// @Tags Asset Rating
// @Produce json
// @Param asset_id query int true "资产 ID | Asset ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["asset.management.read","asset.rating.read"]
// @Router /ratings/stats [get]
func (h *Handler) getRatingStats(c *gin.Context) {
	assetID, err := strconv.ParseInt(c.Query("asset_id"), 10, 64)
	if err != nil || assetID <= 0 {
		commonAPI.BadRequestError(c, commoni18n.T(c, i18nkeys.MsgMissingAssetID))
		return
	}
	stats, err := h.ratingSvc.GetStats(commonAuth.GetTenantID(c), assetID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	commonAPI.SuccessResponse(c, stats)
}

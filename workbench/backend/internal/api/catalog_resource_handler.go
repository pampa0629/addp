package api

import (
	"errors"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	workbenchi18n "github.com/addp/workbench/i18n"
	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/service"
	"github.com/gin-gonic/gin"
)

type CatalogResourceHandler struct {
	service *service.CatalogResourceService
}

func NewCatalogResourceHandler(catalogResourceService *service.CatalogResourceService) *CatalogResourceHandler {
	return &CatalogResourceHandler{service: catalogResourceService}
}

// ListChanges 返回当前 Tenant 可重放的 Data Application 变化。
// @Summary 拉取数据应用目录资源变化 | Pull Data Application catalog resource changes
// @Description 按不透明游标返回已首次发布 Data Application 的最小检索摘要变化，仅供 Catalog 服务消费 | Return minimal-search-summary changes for Data Applications that have been published at least once, for the Catalog service only
// @Tags Catalog Integration
// @Produce json
// @Param after_cursor query string false "上次成功提交的不透明游标，空值从历史起点开始 | Opaque cursor committed by the consumer; empty starts from the beginning"
// @Param limit query int false "批大小，默认 200，最大 500 | Batch size, default 200 and maximum 500"
// @Success 200 {object} models.CatalogResourceChangesResponse
// @Failure 400 {object} map[string]interface{} "无效游标或批大小 | Invalid cursor or batch size"
// @Failure 401 {object} map[string]interface{} "需要认证 | Authentication required"
// @Failure 403 {object} map[string]interface{} "非 Catalog 服务或权限不足 | Catalog service or permission required"
// @Failure 500 {object} map[string]interface{} "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.catalog.read"]
// @Router /catalog-resources/changes [get]
// @Security BearerAuth
func (h *CatalogResourceHandler) ListChanges(c *gin.Context) {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			respondCatalogResourceError(c, service.ErrInvalidCatalogResourceRequest)
			return
		}
		limit = parsed
	}
	tenantID, ok := commonauth.TenantIDFromGin(c)
	if !ok {
		respondCatalogResourceError(c, service.ErrInvalidCatalogResourceRequest)
		return
	}
	result, err := h.service.ListChanges(c.Request.Context(), tenantID, c.Query("after_cursor"), limit)
	if err != nil {
		respondCatalogResourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ResolveReferences 动态解析当前 Tenant 的 Data Application 专业摘要。
// @Summary 解析数据应用目录引用 | Resolve Data Application catalog references
// @Description 按请求顺序动态解析 1 到 200 个已首次发布的数据应用引用；跨 Tenant、不存在和从未发布统一 found=false，仅供 Catalog 服务消费 | Dynamically resolve 1 to 200 Data Application references that have been published at least once; cross-tenant, missing, and never-published references all return found=false; Catalog service only
// @Tags Catalog Integration
// @Accept json
// @Produce json
// @Param request body models.ResolveCatalogReferencesRequest true "数据应用目录引用 | Data Application catalog references"
// @Success 200 {object} models.ResolveCatalogReferencesResponse
// @Failure 400 {object} map[string]interface{} "请求无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "需要认证 | Authentication required"
// @Failure 403 {object} map[string]interface{} "非 Catalog 服务或权限不足 | Catalog service or permission required"
// @Failure 500 {object} map[string]interface{} "解析失败 | Resolution failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.catalog.read"]
// @Router /runtime/catalog-references/resolve [post]
// @Security BearerAuth
func (h *CatalogResourceHandler) ResolveReferences(c *gin.Context) {
	var request models.ResolveCatalogReferencesRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondCatalogResourceError(c, service.ErrInvalidCatalogResourceRequest)
		return
	}
	tenantID, ok := commonauth.TenantIDFromGin(c)
	if !ok {
		respondCatalogResourceError(c, service.ErrInvalidCatalogResourceRequest)
		return
	}
	result, err := h.service.Resolve(c.Request.Context(), tenantID, request.References)
	if err != nil {
		respondCatalogResourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func respondCatalogResourceError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := commoni18n.T(c, workbenchi18n.MsgOperationFailed)
	if errors.Is(err, service.ErrInvalidCatalogResourceRequest) {
		status = http.StatusBadRequest
		message = commoni18n.T(c, workbenchi18n.MsgInvalidRequest)
	}
	commonapi.RespondError(c, status, message)
}

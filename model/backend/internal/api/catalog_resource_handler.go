package api

import (
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type CatalogResourceHandler struct {
	service *service.CatalogResourceService
}

func NewCatalogResourceHandler(service *service.CatalogResourceService) *CatalogResourceHandler {
	return &CatalogResourceHandler{service: service}
}

// ListChanges 返回当前 Tenant 可重放的 Model 专业资源变化。
// @Summary 拉取 Model 目录资源变化 | Pull Model catalog resource changes
// @Description 按不透明游标返回 Entity 与 LogicalTable 的最小摘要变化，仅供 Catalog 服务消费 | Return Entity and LogicalTable minimal-summary changes by opaque cursor for the Catalog service only
// @Tags Catalog Integration
// @Produce json
// @Param after_cursor query string false "上次成功提交的不透明游标，空值从历史起点开始 | Opaque cursor committed by the consumer; empty starts from the beginning"
// @Param limit query int false "批大小，默认 200，最大 500 | Batch size, default 200 and maximum 500"
// @Success 200 {object} models.CatalogResourceChangesResponse "Model 目录资源变化批次 | Model catalog resource change batch"
// @Failure 400 {object} models.ErrorResponse "无效游标或批大小 | Invalid cursor or batch size"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "非 Catalog 服务或权限不足 | Catalog service or permission required"
// @Failure 500 {object} models.ErrorResponse "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.catalog.read"]
// @Router /catalog-resources/changes [get]
// @Security BearerAuth
func (h *CatalogResourceHandler) ListChanges(c *gin.Context) {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
			return
		}
		limit = parsed
	}
	result, err := h.service.ListChanges(c.Request.Context(), getTenantID(c), c.Query("after_cursor"), limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ResolveReferences 动态解析当前 Tenant 的 Model 专业资源摘要。
// @Summary 解析 Model 目录引用 | Resolve Model catalog references
// @Description 按请求顺序动态解析 1 到 200 个 Entity 或 LogicalTable 引用；跨 Tenant 与不存在统一 found=false，仅供 Catalog 服务消费 | Dynamically resolve 1 to 200 Entity or LogicalTable references in request order; cross-tenant and missing references both return found=false; Catalog service only
// @Tags Catalog Integration
// @Accept json
// @Produce json
// @Param request body models.ResolveCatalogReferencesRequest true "Model 目录引用 | Model catalog references"
// @Success 200 {object} models.ResolveCatalogReferencesResponse "当前 Model 专业摘要 | Current Model professional summaries"
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "非 Catalog 服务或权限不足 | Catalog service or permission required"
// @Failure 500 {object} models.ErrorResponse "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.catalog.read"]
// @Router /runtime/catalog-references/resolve [post]
// @Security BearerAuth
func (h *CatalogResourceHandler) ResolveReferences(c *gin.Context) {
	var request models.ResolveCatalogReferencesRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.service.Resolve(c.Request.Context(), getTenantID(c), request.References)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

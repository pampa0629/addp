package api

import (
	"errors"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

type CatalogResourceHandler struct {
	service *service.CatalogResourceService
}

func NewCatalogResourceHandler(catalogResourceService *service.CatalogResourceService) *CatalogResourceHandler {
	return &CatalogResourceHandler{service: catalogResourceService}
}

// ListChanges 返回当前 Tenant 可重放的 QueryService 变化。
// @Summary 拉取 QueryService 目录资源变化 | Pull QueryService catalog resource changes
// @Description 按不透明游标返回 QueryService 的最小检索摘要变化，仅供 Catalog 服务消费 | Return QueryService minimal-search-summary changes by opaque cursor for the Catalog service only
// @Tags Catalog Integration
// @Produce json
// @Param after_cursor query string false "上次成功提交的不透明游标，空值从历史起点开始 | Opaque cursor committed by the consumer; empty starts from the beginning"
// @Param limit query int false "批大小，默认 200，最大 500 | Batch size, default 200 and maximum 500"
// @Success 200 {object} models.CatalogResourceChangesResponse
// @Failure 400 {object} map[string]string "无效游标或批大小 | Invalid cursor or batch size"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "非 Catalog 服务或权限不足 | Catalog service or permission required"
// @Failure 500 {object} map[string]string "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.catalog.read"]
// @Router /catalog-resources/changes [get]
// @Security BearerAuth
func (h *CatalogResourceHandler) ListChanges(c *gin.Context) {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": service.ErrInvalidCatalogResourceRequest.Error()})
			return
		}
		limit = parsed
	}
	result, err := h.service.ListChanges(c.Request.Context(), int64(tenantIDValue(c)), c.Query("after_cursor"), limit)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrInvalidCatalogResourceRequest) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ResolveReferences 动态解析当前 Tenant 的 QueryService 专业摘要。
// @Summary 解析 QueryService 目录引用 | Resolve QueryService catalog references
// @Description 按请求顺序动态解析 1 到 200 个 QueryService 引用；跨 Tenant 与不存在统一 found=false，仅供 Catalog 服务消费 | Dynamically resolve 1 to 200 QueryService references in request order; cross-tenant and missing references both return found=false; Catalog service only
// @Tags Catalog Integration
// @Accept json
// @Produce json
// @Param request body models.ResolveCatalogReferencesRequest true "QueryService 目录引用 | QueryService catalog references"
// @Success 200 {object} models.ResolveCatalogReferencesResponse
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "非 Catalog 服务或权限不足 | Catalog service or permission required"
// @Failure 500 {object} map[string]string "解析失败 | Resolution failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.catalog.read"]
// @Router /runtime/catalog-references/resolve [post]
// @Security BearerAuth
func (h *CatalogResourceHandler) ResolveReferences(c *gin.Context) {
	var request models.ResolveCatalogReferencesRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": service.ErrInvalidCatalogResourceRequest.Error()})
		return
	}
	result, err := h.service.Resolve(c.Request.Context(), int64(tenantIDValue(c)), request.References)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrInvalidCatalogResourceRequest) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

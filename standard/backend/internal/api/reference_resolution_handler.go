package api

import (
	"errors"
	"net/http"

	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

type ReferenceResolutionHandler struct {
	service *service.ReferenceResolutionService
}

type referenceResolutionRequest struct {
	References []service.ReferenceResolutionRequest `json:"references" binding:"required,min=1,max=200"`
}

type referenceResolutionResponse struct {
	Results []service.ReferenceResolution `json:"results"`
}

func NewReferenceResolutionHandler(resolutionService *service.ReferenceResolutionService) *ReferenceResolutionHandler {
	return &ReferenceResolutionHandler{service: resolutionService}
}

// Resolve 精确解析 Catalog 提交的 Standard 引用。
// @Summary 精确批量解析 Standard 引用 | Resolve exact Standard references in batch
// @Description 仅 addp-catalog Tenant Service Principal 可按当前 Tenant 解析 Domain、Glossary 和 Element；跨 Tenant 与不存在统一返回 found=false | Only the addp-catalog tenant service principal may resolve domains, glossaries, and elements in the current tenant; cross-tenant and missing references both return found=false
// @Tags CatalogReferences
// @Accept json
// @Produce json
// @Param request body referenceResolutionRequest true "精确引用集合，最多 200 个 | Exact references, up to 200"
// @Success 200 {object} referenceResolutionResponse
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "仅允许 addp-catalog 且需三个读取权限 | addp-catalog and all three read permissions required"
// @Failure 500 {object} map[string]string "解析失败 | Resolution failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.domain.read","standard.glossary.read","standard.element.read"]
// @Router /references/resolve [post]
// @Security BearerAuth
func (h *ReferenceResolutionHandler) Resolve(c *gin.Context) {
	var request referenceResolutionRequest
	if h == nil || h.service == nil || c.ShouldBindJSON(&request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidReferenceResolutionRequest)
		return
	}
	results, err := h.service.Resolve(c.Request.Context(), getTenantID(c), request.References)
	if err != nil {
		if errors.Is(err, service.ErrInvalidReferenceResolutionRequest) {
			respondError(c, http.StatusBadRequest, err)
			return
		}
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, referenceResolutionResponse{Results: results})
}

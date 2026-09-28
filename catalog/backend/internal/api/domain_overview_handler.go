package api

import (
	"net/http"

	"github.com/addp/catalog/internal/service"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

// ListDomainOverviews dynamically projects Standard's current Domain hierarchy.
// @Summary 查询企业目录业务域概况 | List catalog business domain overviews
// @Description 使用 Catalog 运行身份动态读取 Standard 当前业务域，只公开名称、编码、定义和层级；不授予 Standard 专业详情权限，不保存副本 | Dynamically read current Standard domains using Catalog's service identity, exposing only name, code, description and hierarchy; does not grant Standard detail access or store copies
// @Tags Catalog
// @Produce json
// @Success 200 {array} service.DomainOverview "当前业务域概况 | Current domain overviews"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 503 {object} map[string]interface{} "Standard 当前不可达 | Standard currently unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @Router /domains [get]
// @Security BearerAuth
func (h *Handler) ListDomainOverviews(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok || h.domains == nil {
		respondError(c, http.StatusServiceUnavailable, service.ErrDomainOverviewUnavailable)
		return
	}
	domains, err := h.domains.ListDomainOverviews(c.Request.Context(), tenantID)
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, service.ErrDomainOverviewUnavailable)
		return
	}
	c.JSON(http.StatusOK, domains)
}

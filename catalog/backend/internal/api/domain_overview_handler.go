package api

import (
	"errors"
	"github.com/addp/catalog/internal/service"
	"github.com/addp/common/domainreference"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// GetDomainResourceOverview returns the same visible Catalog facts as resource browsing.
// @Summary 查询业务域目录总览 | Get domain catalog overview
// @Tags Catalog Entries
// @Produce json
// @Param id path string true "精确业务域 ID | Exact domain ID"
// @Param view query string false "查看范围 | View" Enums(governance,inventory) default(governance)
// @Success 200 {object} service.DomainResourceOverview
// @Failure 400 {object} map[string]interface{} "无效查询 | Invalid query"
// @Failure 401 {object} map[string]string "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 500 {object} map[string]interface{} "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @x-addp-conditional-permissions ["catalog.inventory.read"]
// @Router /domains/{id}/overview [get]
// @Security BearerAuth
func (h *Handler) GetDomainResourceOverview(c *gin.Context) {
	tenantID, _ := commonAuth.TenantIDFromGin(c)
	h.sync.ObserveTenant(tenantID)
	domainID, err := parseOptionalCanonicalPositiveInt64(c.Param("id"))
	if err != nil || domainID == 0 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	result, err := h.entries.GetDomainResourceOverview(c.Request.Context(), tenantID, entryAccess(c), domainID, c.Query("view"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListDomainStandardReferences reads explicit local evidence, grouped by standard identity.
// @Summary 查询业务域目录标准引用 | List domain catalog standard references
// @Tags Catalog Entries
// @Produce json
// @Param domain_id query string true "精确业务域 ID | Exact domain ID"
// @Param view query string false "查看范围 | View" Enums(governance,inventory) default(governance)
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数，上限100 | Page size, max 100" default(20)
// @Success 200 {object} domainreference.Page
// @Failure 400 {object} map[string]interface{} "无效查询 | Invalid query"
// @Failure 401 {object} map[string]string "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 500 {object} map[string]interface{} "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @x-addp-conditional-permissions ["catalog.inventory.read"]
// @Router /domain_standard_references [get]
// @Security BearerAuth
func (h *Handler) ListDomainStandardReferences(c *gin.Context) {
	tenantID, _ := commonAuth.TenantIDFromGin(c)
	h.sync.ObserveTenant(tenantID)
	domainID, err := parseOptionalCanonicalPositiveInt64(c.Query("domain_id"))
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || domainID == 0 || e1 != nil || e2 != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	result, err := h.entries.ListDomainStandardReferences(c.Request.Context(), tenantID, entryAccess(c), domainID, c.Query("view"), page, size)
	if errors.Is(err, domainreference.ErrInvalidQuery) {
		err = service.ErrInvalidPage
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListDomainOverviews dynamically projects Standard's current Domain hierarchy.
// @Summary 查询企业目录业务域概况 | List catalog business domain overviews
// @Description 使用 Catalog 运行身份动态读取 Standard 当前业务域，只公开名称、编码、定义和层级；不授予 Standard 专业详情权限，不保存副本 | Dynamically read current Standard domains using Catalog's service identity, exposing only name, code, description and hierarchy; does not grant Standard detail access or store copies
// @Tags Catalog
// @Produce json
// @Success 200 {array} service.DomainOverview "当前业务域概况 | Current domain overviews"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 401 {object} map[string]string "未认证 | Unauthorized"
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

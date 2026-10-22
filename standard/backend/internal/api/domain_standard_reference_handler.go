package api

import (
	"errors"
	"github.com/addp/common/domainreference"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type DomainStandardReferenceHandler struct {
	service *service.CatalogResourceService
}

func NewDomainStandardReferenceHandler(s *service.CatalogResourceService) *DomainStandardReferenceHandler {
	return &DomainStandardReferenceHandler{service: s}
}

func (h *DomainStandardReferenceHandler) list(c *gin.Context, kind string) {
	domainRaw := c.Query("domain_id")
	domainID, err := strconv.ParseInt(domainRaw, 10, 64)
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || domainID <= 0 || strconv.FormatInt(domainID, 10) != domainRaw || e1 != nil || e2 != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidCatalogResourceRequest)
		return
	}
	result, err := h.service.ListDomainStandardReferences(c.Request.Context(), getTenantID(c), domainID, kind, page, size)
	if errors.Is(err, domainreference.ErrInvalidQuery) {
		respondError(c, http.StatusBadRequest, service.ErrInvalidCatalogResourceRequest)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListGlossary reads explicit domain-owned standard references.
// @Summary 查询业务域标准引用（glossary） | List domain standard references (glossary)
// @Tags Domain References
// @Produce json
// @Param domain_id query string true "精确业务域 ID | Exact domain ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数，上限100 | Page size, max 100" default(20)
// @Success 200 {object} domainreference.Page
// @Failure 400 {object} map[string]string "查询无效 | Invalid query"
// @Failure 401 {object} map[string]string "未认证 | Unauthorized"
// @Failure 403 {object} map[string]string "无权查看 | Forbidden"
// @Failure 500 {object} map[string]string "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.read"]
// @Router /glossaries/standard_references [get]
// @Security BearerAuth
func (h *DomainStandardReferenceHandler) ListGlossary(c *gin.Context) { h.list(c, "glossary") }

// ListElement reads explicit domain-owned standard references.
// @Summary 查询业务域标准引用（element） | List domain standard references (element)
// @Tags Domain References
// @Produce json
// @Param domain_id query string true "精确业务域 ID | Exact domain ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数，上限100 | Page size, max 100" default(20)
// @Success 200 {object} domainreference.Page
// @Failure 400 {object} map[string]string "查询无效 | Invalid query"
// @Failure 401 {object} map[string]string "未认证 | Unauthorized"
// @Failure 403 {object} map[string]string "无权查看 | Forbidden"
// @Failure 500 {object} map[string]string "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /elements/standard_references [get]
// @Security BearerAuth
func (h *DomainStandardReferenceHandler) ListElement(c *gin.Context) { h.list(c, "element") }

// ListMetric reads explicit domain-owned standard references.
// @Summary 查询业务域标准引用（metric） | List domain standard references (metric)
// @Tags Domain References
// @Produce json
// @Param domain_id query string true "精确业务域 ID | Exact domain ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数，上限100 | Page size, max 100" default(20)
// @Success 200 {object} domainreference.Page
// @Failure 400 {object} map[string]string "查询无效 | Invalid query"
// @Failure 401 {object} map[string]string "未认证 | Unauthorized"
// @Failure 403 {object} map[string]string "无权查看 | Forbidden"
// @Failure 500 {object} map[string]string "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metrics/standard_references [get]
// @Security BearerAuth
func (h *DomainStandardReferenceHandler) ListMetric(c *gin.Context) { h.list(c, "metric") }

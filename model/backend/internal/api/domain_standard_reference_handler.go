package api

import (
	"errors"
	"github.com/addp/common/domainreference"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// Keep the shared error DTO visible to Swagger without a duplicate schema.
var _ models.ErrorResponse

type DomainStandardReferenceHandler struct{ service *service.EntityService }

func NewDomainStandardReferenceHandler(s *service.EntityService) *DomainStandardReferenceHandler {
	return &DomainStandardReferenceHandler{service: s}
}

func (h *DomainStandardReferenceHandler) list(c *gin.Context, kind string) {
	domainRaw := c.Query("domain_id")
	domainID, err := strconv.ParseInt(domainRaw, 10, 64)
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || domainID <= 0 || strconv.FormatInt(domainID, 10) != domainRaw || e1 != nil || e2 != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.service.ListDomainStandardReferences(c.Request.Context(), getTenantID(c), domainID, kind, page, size)
	if errors.Is(err, domainreference.ErrInvalidQuery) {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListEntity reads explicit domain-owned standard references.
// @Summary 查询业务域标准引用（entity） | List domain standard references (entity)
// @Tags Domain References
// @Produce json
// @Param domain_id query string true "精确业务域 ID | Exact domain ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数，上限100 | Page size, max 100" default(20)
// @Success 200 {object} domainreference.Page
// @Failure 400 {object} models.ErrorResponse "查询无效 | Invalid query"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权查看 | Forbidden"
// @Failure 500 {object} models.ErrorResponse "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read"]
// @Router /entities/standard_references [get]
// @Security BearerAuth
func (h *DomainStandardReferenceHandler) ListEntity(c *gin.Context) { h.list(c, "entity") }

// ListLogicalTable reads explicit domain-owned standard references.
// @Summary 查询业务域标准引用（logical_table） | List domain standard references (logical_table)
// @Tags Domain References
// @Produce json
// @Param domain_id query string true "精确业务域 ID | Exact domain ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数，上限100 | Page size, max 100" default(20)
// @Success 200 {object} domainreference.Page
// @Failure 400 {object} models.ErrorResponse "查询无效 | Invalid query"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权查看 | Forbidden"
// @Failure 500 {object} models.ErrorResponse "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/standard_references [get]
// @Security BearerAuth
func (h *DomainStandardReferenceHandler) ListLogicalTable(c *gin.Context) { h.list(c, "logical_table") }

// ListMetricImplementation reads explicit domain-owned standard references.
// @Summary 查询业务域标准引用（metric_implementation） | List domain standard references (metric_implementation)
// @Tags Domain References
// @Produce json
// @Param domain_id query string true "精确业务域 ID | Exact domain ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页条数，上限100 | Page size, max 100" default(20)
// @Success 200 {object} domainreference.Page
// @Failure 400 {object} models.ErrorResponse "查询无效 | Invalid query"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权查看 | Forbidden"
// @Failure 500 {object} models.ErrorResponse "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.metric_implementation.read"]
// @Router /metric-implementations/standard_references [get]
// @Security BearerAuth
func (h *DomainStandardReferenceHandler) ListMetricImplementation(c *gin.Context) {
	h.list(c, "metric_implementation")
}

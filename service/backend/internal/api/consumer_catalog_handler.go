package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/logger"
	authmiddleware "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	servicei18n "github.com/addp/service/i18n"
	"github.com/addp/service/internal/models"
	serviceinternal "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

type consumerCatalog interface {
	ListQueryServices(filter models.ConsumerServiceListFilter) ([]models.ConsumerServiceSummary, int64, error)
	GetQueryService(tenantID, serviceID uint) (*models.ConsumerDescriptor, error)
}

type ConsumerCatalogHandler struct {
	catalog consumerCatalog
}

func NewConsumerCatalogHandler(catalog *serviceinternal.ConsumerCatalogService) *ConsumerCatalogHandler {
	return &ConsumerCatalogHandler{catalog: catalog}
}

// ListServices godoc
// @Summary 可消费服务列表 | List consumable services
// @Tags ServiceConsumer
// @Produce json
// @Param search query string false "搜索词 | Search"
// @Param service_type query string false "服务类型 | Service type" Enums(query,graph,tile,registered)
// @Param output_kind query string false "输出类型 | Output kind" Enums(tabular,spatial_tabular)
// @Param page query int false "页码 | Page" default(1) minimum(1)
// @Param page_size query int false "每页数量 | Page size" default(20) minimum(1) maximum(100)
// @Success 200 {object} models.ConsumerServiceListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.data_read.execute"]
// @Router /consumer/services [get]
// @Security BearerAuth
func (h *ConsumerCatalogHandler) ListServices(c *gin.Context) {
	page, pageSize, ok := consumerPagination(c)
	if !ok {
		return
	}
	search := strings.TrimSpace(c.Query("search"))
	if len([]rune(search)) > 200 {
		writeConsumerError(c, http.StatusBadRequest, servicei18n.MsgConsumerFilterInvalid, "consumer_filter_invalid")
		return
	}
	serviceType := strings.TrimSpace(c.Query("service_type"))
	if serviceType != "" && !validConsumerServiceType(serviceType) {
		writeConsumerError(c, http.StatusBadRequest, servicei18n.MsgConsumerFilterInvalid, "consumer_filter_invalid")
		return
	}
	outputKind := strings.TrimSpace(c.Query("output_kind"))
	if outputKind != "" && outputKind != models.ConsumerOutputKindTabular && outputKind != models.ConsumerOutputKindSpatialTabular {
		writeConsumerError(c, http.StatusBadRequest, servicei18n.MsgConsumerFilterInvalid, "consumer_filter_invalid")
		return
	}

	// Phase 1 exposes only Query Service. Known future types are valid filters
	// but currently produce an empty, non-leaking catalog page.
	if serviceType != "" && serviceType != models.ConsumerServiceTypeQuery {
		c.JSON(http.StatusOK, models.ConsumerServiceListResponse{
			Data: []models.ConsumerServiceSummary{}, Page: page, PageSize: pageSize,
		})
		return
	}
	tenantID := authmiddleware.GetTenantID(c)
	items, total, err := h.catalog.ListQueryServices(models.ConsumerServiceListFilter{
		TenantID: tenantID, Search: search, OutputKind: outputKind,
		Offset: (page - 1) * pageSize, Limit: pageSize,
	})
	if err != nil {
		logger.L().Error("读取服务消费目录失败", "error", err, "tenant_id", tenantID)
		writeConsumerError(c, http.StatusInternalServerError, servicei18n.MsgConsumerCatalogFailed, "consumer_catalog_failed")
		return
	}
	c.JSON(http.StatusOK, models.ConsumerServiceListResponse{
		Data: items, Total: total, Page: page, PageSize: pageSize,
		TotalPages: (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// GetService godoc
// @Summary 获取服务消费描述 | Get service consumer descriptor
// @Tags ServiceConsumer
// @Produce json
// @Param service_type path string true "服务类型 | Service type" Enums(query,graph,tile,registered)
// @Param service_id path int true "服务 ID | Service ID" minimum(1)
// @Success 200 {object} models.ConsumerDescriptor
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.data_read.execute"]
// @Router /consumer/services/{service_type}/{service_id} [get]
// @Security BearerAuth
func (h *ConsumerCatalogHandler) GetService(c *gin.Context) {
	serviceType := c.Param("service_type")
	if !validConsumerServiceType(serviceType) {
		writeConsumerError(c, http.StatusBadRequest, servicei18n.MsgConsumerReferenceInvalid, "consumer_reference_invalid")
		return
	}
	serviceID, err := strconv.ParseUint(c.Param("service_id"), 10, 32)
	if err != nil || serviceID == 0 {
		writeConsumerError(c, http.StatusBadRequest, servicei18n.MsgConsumerReferenceInvalid, "consumer_reference_invalid")
		return
	}
	if serviceType != models.ConsumerServiceTypeQuery {
		writeConsumerError(c, http.StatusNotFound, servicei18n.MsgServiceNotFound, "consumer_service_not_found")
		return
	}
	descriptor, err := h.catalog.GetQueryService(authmiddleware.GetTenantID(c), uint(serviceID))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			writeConsumerError(c, http.StatusNotFound, servicei18n.MsgServiceNotFound, "consumer_service_not_found")
			return
		}
		logger.L().Error("读取服务消费描述失败", "error", err, "service_type", serviceType, "service_id", serviceID)
		writeConsumerError(c, http.StatusInternalServerError, servicei18n.MsgConsumerCatalogFailed, "consumer_catalog_failed")
		return
	}
	c.JSON(http.StatusOK, descriptor)
}

// RequireTenantQueryExecutionPermission is the first Service owner resource
// policy. Until services have explicit Resource Scope Bindings, a scoped
// department/project-group Assignment must fail closed instead of expanding to
// every Service in the tenant.
func RequireTenantQueryExecutionPermission(c *gin.Context) {
	if !hasTenantQueryExecutionPermission(c, authmiddleware.GetTenantID(c)) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": "permission_denied",
		})
		return
	}
	c.Next()
}

func hasTenantQueryExecutionPermission(c *gin.Context, tenantID uint) bool {
	if tenantID == 0 {
		return false
	}
	for _, scope := range authmiddleware.RolePermissionScopes(c, "service.data_read.execute") {
		if scope.Type == "tenant" && scope.TenantID != nil && *scope.TenantID == strconv.FormatUint(uint64(tenantID), 10) {
			return true
		}
	}
	return false
}

func validConsumerServiceType(value string) bool {
	switch value {
	case models.ConsumerServiceTypeQuery, models.ConsumerServiceTypeGraph,
		models.ConsumerServiceTypeTile, models.ConsumerServiceTypeRegistered:
		return true
	default:
		return false
	}
}

func consumerPagination(c *gin.Context) (int, int, bool) {
	page, pageSize := 1, 20
	if raw := c.Query("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			writeConsumerError(c, http.StatusBadRequest, servicei18n.MsgConsumerFilterInvalid, "consumer_filter_invalid")
			return 0, 0, false
		}
		page = value
	}
	if raw := c.Query("page_size"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 || value > 100 {
			writeConsumerError(c, http.StatusBadRequest, servicei18n.MsgConsumerFilterInvalid, "consumer_filter_invalid")
			return 0, 0, false
		}
		pageSize = value
	}
	return page, pageSize, true
}

func writeConsumerError(c *gin.Context, status int, messageKey, errorCode string) {
	c.JSON(status, gin.H{"error": commoni18n.T(c, messageKey), "error_code": errorCode})
}

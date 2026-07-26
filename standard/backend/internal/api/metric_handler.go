package api

import (
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

// MetricHandler 指标 Handler
type MetricHandler struct {
	svc *service.MetricService
}

func NewMetricHandler(svc *service.MetricService) *MetricHandler {
	return &MetricHandler{svc: svc}
}

// --- 指标目录 ---

// @Summary 获取指标分类列表 | List metric categories
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metric-categories [get]
// @Security BearerAuth
func (h *MetricHandler) ListCategories(c *gin.Context) {
	tenantID := getTenantID(c)
	cats, err := h.svc.ListCategories(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cats)
}

// @Summary 创建指标分类 | Create metric category
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.create"]
// @Router /metric-categories [post]
// @Security BearerAuth
func (h *MetricHandler) CreateCategory(c *gin.Context) {
	var req models.CreateMetricCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	cat, err := h.svc.CreateCategory(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, cat)
}

// @Summary 更新指标分类 | Update metric category
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update"]
// @Router /metric-categories/{id} [put]
// @Security BearerAuth
func (h *MetricHandler) UpdateCategory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.UpdateMetricCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	cat, err := h.svc.UpdateCategory(id, tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cat)
}

// @Summary 删除指标分类 | Delete metric category
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.delete"]
// @Router /metric-categories/{id} [delete]
// @Security BearerAuth
func (h *MetricHandler) DeleteCategory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteCategory(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// --- 指标定义 ---

// @Summary 获取指标列表 | List metrics
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metrics [get]
// @Security BearerAuth
func (h *MetricHandler) ListMetrics(c *gin.Context) {
	tenantID := getTenantID(c)
	opts := repository.ListMetricOptions{
		Type:    c.Query("type"),
		Status:  c.Query("status"),
		Keyword: c.Query("keyword"),
	}
	if catIDStr := c.Query("category_id"); catIDStr != "" {
		if id, err := strconv.ParseInt(catIDStr, 10, 64); err == nil {
			opts.CategoryID = &id
		}
	}
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil {
			opts.Page = p
		}
	}
	if psStr := c.Query("page_size"); psStr != "" {
		if ps, err := strconv.Atoi(psStr); err == nil {
			opts.PageSize = ps
		}
	}
	metrics, total, err := h.svc.ListMetrics(tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	page := opts.Page
	pageSize := opts.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	c.JSON(http.StatusOK, gin.H{"data": metrics, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages})
}

// @Summary 获取指标详情 | Get metric detail
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metrics/{id} [get]
// @Security BearerAuth
func (h *MetricHandler) GetMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	metric, err := h.svc.GetMetric(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgMetricNotFound)})
		return
	}

	elements, _ := h.svc.GetElementMappings(id)
	deps, _ := h.svc.GetDependencies(id)

	type MetricDetailResponse struct {
		*models.Metric
		ElementIDs    []int64 `json:"element_ids"`
		DependencyIDs []int64 `json:"dependency_ids"`
	}
	c.JSON(http.StatusOK, MetricDetailResponse{
		Metric:        metric,
		ElementIDs:    extractElementIDs(elements),
		DependencyIDs: extractDependencyIDs(deps),
	})
}

// @Summary 创建指标 | Create metric
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.create"]
// @Router /metrics [post]
// @Security BearerAuth
func (h *MetricHandler) CreateMetric(c *gin.Context) {
	var req models.CreateMetricRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	metric, err := h.svc.CreateMetric(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, metric)
}

// @Summary 更新指标 | Update metric
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update"]
// @Router /metrics/{id} [put]
// @Security BearerAuth
func (h *MetricHandler) UpdateMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.UpdateMetricRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	metric, err := h.svc.UpdateMetric(id, tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, metric)
}

// @Summary 删除指标 | Delete metric
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.delete"]
// @Router /metrics/{id} [delete]
// @Security BearerAuth
func (h *MetricHandler) DeleteMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteMetric(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// @Summary 审批指标 | Approve metric
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.approve"]
// @Router /metrics/{id}/approve [post]
// @Security BearerAuth
func (h *MetricHandler) ApproveMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	if err := h.svc.ApproveMetric(id, tenantID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgApproveSuccess)})
}

// @Summary 废弃指标 | Deprecate metric
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.offline"]
// @Router /metrics/{id}/deprecate [post]
// @Security BearerAuth
func (h *MetricHandler) DeprecateMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	if err := h.svc.DeprecateMetric(id, tenantID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeprecateSuccess)})
}

// 辅助函数
func extractElementIDs(mappings []models.MetricElementMapping) []int64 {
	ids := make([]int64, 0, len(mappings))
	for _, m := range mappings {
		ids = append(ids, m.ElementID)
	}
	return ids
}

func extractDependencyIDs(deps []models.MetricDependency) []int64 {
	ids := make([]int64, 0, len(deps))
	for _, d := range deps {
		ids = append(ids, d.ToMetricID)
	}
	return ids
}

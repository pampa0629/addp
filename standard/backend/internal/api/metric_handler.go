package api

import (
	"net/http"
	"strconv"

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

func (h *MetricHandler) ListCategories(c *gin.Context) {
	tenantID := getTenantID(c)
	cats, err := h.svc.ListCategories(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cats})
}

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
	c.JSON(http.StatusCreated, gin.H{"data": cat})
}

func (h *MetricHandler) UpdateCategory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
	c.JSON(http.StatusOK, gin.H{"data": cat})
}

func (h *MetricHandler) DeleteCategory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteCategory(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// --- 指标定义 ---

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
	c.JSON(http.StatusOK, gin.H{"data": metrics, "total": total})
}

func (h *MetricHandler) GetMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	metric, err := h.svc.GetMetric(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "metric not found"})
		return
	}

	// 附带关联信息
	elements, _ := h.svc.GetElementMappings(id)
	deps, _ := h.svc.GetDependencies(id)

	c.JSON(http.StatusOK, gin.H{
		"data":          metric,
		"element_ids":   extractElementIDs(elements),
		"dependency_ids": extractDependencyIDs(deps),
	})
}

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
	c.JSON(http.StatusCreated, gin.H{"data": metric})
}

func (h *MetricHandler) UpdateMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
	c.JSON(http.StatusOK, gin.H{"data": metric})
}

func (h *MetricHandler) DeleteMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteMetric(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *MetricHandler) ApproveMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	if err := h.svc.ApproveMetric(id, tenantID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "approved"})
}

func (h *MetricHandler) DeprecateMetric(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	if err := h.svc.DeprecateMetric(id, tenantID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deprecated"})
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

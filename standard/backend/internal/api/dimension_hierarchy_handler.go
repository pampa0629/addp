package api

import (
	"net/http"
	"strconv"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

// DimensionHierarchyHandler 维度层级 Handler
type DimensionHierarchyHandler struct {
	svc *service.DimensionHierarchyService
}

func NewDimensionHierarchyHandler(svc *service.DimensionHierarchyService) *DimensionHierarchyHandler {
	return &DimensionHierarchyHandler{svc: svc}
}

// List GET /api/standard/dimension-hierarchies
func (h *DimensionHierarchyHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	list, err := h.svc.List(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// Get GET /api/standard/dimension-hierarchies/:id
func (h *DimensionHierarchyHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	item, err := h.svc.GetByID(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "dimension hierarchy not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

// Create POST /api/standard/dimension-hierarchies
func (h *DimensionHierarchyHandler) Create(c *gin.Context) {
	var req models.CreateDimensionHierarchyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	item, err := h.svc.Create(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": item})
}

// Update PUT /api/standard/dimension-hierarchies/:id
func (h *DimensionHierarchyHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req models.UpdateDimensionHierarchyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	item, err := h.svc.Update(id, tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

// Delete DELETE /api/standard/dimension-hierarchies/:id
func (h *DimensionHierarchyHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.Delete(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// --- 层级管理 ---

// ListLevels GET /api/standard/dimension-hierarchies/:id/levels
func (h *DimensionHierarchyHandler) ListLevels(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	levels, err := h.svc.GetLevels(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": levels})
}

// CreateLevel POST /api/standard/dimension-hierarchies/:id/levels
func (h *DimensionHierarchyHandler) CreateLevel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req models.UpsertHierarchyLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	level, err := h.svc.CreateLevel(id, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": level})
}

// UpdateLevel PUT /api/standard/dimension-hierarchies/:id/levels/:lid
func (h *DimensionHierarchyHandler) UpdateLevel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	lid, err := strconv.ParseInt(c.Param("lid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid level id"})
		return
	}
	var req models.UpsertHierarchyLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	level, err := h.svc.UpdateLevel(lid, id, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": level})
}

// DeleteLevel DELETE /api/standard/dimension-hierarchies/:id/levels/:lid
func (h *DimensionHierarchyHandler) DeleteLevel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	lid, err := strconv.ParseInt(c.Param("lid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid level id"})
		return
	}
	if err := h.svc.DeleteLevel(lid, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

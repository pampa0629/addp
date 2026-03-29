package api

import (
	"net/http"
	"strconv"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

// UnitHandler 计量单位 + 度量类别 Handler
type UnitHandler struct {
	svc *service.UnitService
}

func NewUnitHandler(svc *service.UnitService) *UnitHandler {
	return &UnitHandler{svc: svc}
}

// --- 度量类别 ---

// @Summary ListCategories
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /listcategories [get]
// @Security BearerAuth
func (h *UnitHandler) ListCategories(c *gin.Context) {
	tenantID := getTenantID(c)
	cats, err := h.svc.ListCategories(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cats)
}

// @Summary CreateCategory
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /createcategory [get]
// @Security BearerAuth
func (h *UnitHandler) CreateCategory(c *gin.Context) {
	var req models.CreateMeasurementCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	cat, err := h.svc.CreateCategory(&req, tenantID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, cat)
}

// @Summary UpdateCategory
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /updatecategory [get]
// @Security BearerAuth
func (h *UnitHandler) UpdateCategory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req models.UpdateMeasurementCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	cat, err := h.svc.UpdateCategory(id, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cat)
}

// @Summary DeleteCategory
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /deletecategory [get]
// @Security BearerAuth
func (h *UnitHandler) DeleteCategory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteCategory(id, tenantID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// --- 计量单位 ---

// @Summary ListUnits
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /listunits [get]
// @Security BearerAuth
func (h *UnitHandler) ListUnits(c *gin.Context) {
	tenantID := getTenantID(c)
	var categoryID *int64
	if catIDStr := c.Query("category_id"); catIDStr != "" {
		if id, err := strconv.ParseInt(catIDStr, 10, 64); err == nil {
			categoryID = &id
		}
	}
	units, err := h.svc.ListUnits(tenantID, categoryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, units)
}

// @Summary GetUnit
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /getunit [get]
// @Security BearerAuth
func (h *UnitHandler) GetUnit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	unit, err := h.svc.GetUnit(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unit not found"})
		return
	}
	c.JSON(http.StatusOK, unit)
}

// @Summary CreateUnit
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /createunit [get]
// @Security BearerAuth
func (h *UnitHandler) CreateUnit(c *gin.Context) {
	var req models.CreateUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	unit, err := h.svc.CreateUnit(&req, tenantID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, unit)
}

// @Summary UpdateUnit
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /updateunit [get]
// @Security BearerAuth
func (h *UnitHandler) UpdateUnit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req models.UpdateUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	unit, err := h.svc.UpdateUnit(id, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, unit)
}

// @Summary DeleteUnit
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /deleteunit [get]
// @Security BearerAuth
func (h *UnitHandler) DeleteUnit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteUnit(id, tenantID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}


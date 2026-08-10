package api

import (
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
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

// @Summary 获取度量类别列表 | List measurement categories
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.read"]
// @Router /measurement-categories [get]
// @Security BearerAuth
func (h *UnitHandler) ListCategories(c *gin.Context) {
	tenantID := getTenantID(c)
	cats, err := h.svc.ListCategories(tenantID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, cats)
}

// @Summary 创建度量类别 | Create measurement category
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.create"]
// @Router /measurement-categories [post]
// @Security BearerAuth
func (h *UnitHandler) CreateCategory(c *gin.Context) {
	var req models.CreateMeasurementCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	tenantID := getTenantID(c)
	cat, err := h.svc.CreateCategory(&req, tenantID)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, cat)
}

// @Summary 更新度量类别 | Update measurement category
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.update"]
// @Router /measurement-categories/{id} [put]
// @Security BearerAuth
func (h *UnitHandler) UpdateCategory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.UpdateMeasurementCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	tenantID := getTenantID(c)
	cat, err := h.svc.UpdateCategory(id, tenantID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, cat)
}

// @Summary 删除度量类别 | Delete measurement category
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.delete"]
// @Router /measurement-categories/{id} [delete]
// @Security BearerAuth
func (h *UnitHandler) DeleteCategory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteCategory(id, tenantID); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// --- 计量单位 ---

// @Summary 获取计量单位列表 | List units
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.read"]
// @Router /units [get]
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
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, units)
}

// @Summary 获取计量单位详情 | Get unit detail
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.read"]
// @Router /units/{id} [get]
// @Security BearerAuth
func (h *UnitHandler) GetUnit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	unit, err := h.svc.GetUnit(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgUnitNotFound)})
		return
	}
	c.JSON(http.StatusOK, unit)
}

// @Summary 创建计量单位 | Create unit
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.create"]
// @Router /units [post]
// @Security BearerAuth
func (h *UnitHandler) CreateUnit(c *gin.Context) {
	var req models.CreateUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	tenantID := getTenantID(c)
	unit, err := h.svc.CreateUnit(&req, tenantID)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, unit)
}

// @Summary 更新计量单位 | Update unit
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.update"]
// @Router /units/{id} [put]
// @Security BearerAuth
func (h *UnitHandler) UpdateUnit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.UpdateUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	tenantID := getTenantID(c)
	unit, err := h.svc.UpdateUnit(id, tenantID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, unit)
}

// @Summary 删除计量单位 | Delete unit
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.delete"]
// @Router /units/{id} [delete]
// @Security BearerAuth
func (h *UnitHandler) DeleteUnit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteUnit(id, tenantID); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

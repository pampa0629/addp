package api

import (
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
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
// @Success 200 {array} models.MeasurementCategory
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.read"]
// @Router /measurement-categories [get]
// @Security BearerAuth
func (h *UnitHandler) ListCategories(c *gin.Context) {
	owner, ok := unitContext(c)
	if !ok {
		return
	}
	cats, err := h.svc.ListCategories(owner)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	for i := range cats {
		cats[i].Localize(commoni18n.GetLang(c))
	}
	c.JSON(http.StatusOK, cats)
}

// @Summary 创建度量类别 | Create measurement category
// @Tags Standard
// @Produce json
// @Param request body models.CreateMeasurementCategoryRequest true "度量类别 | Measurement category"
// @Success 201 {object} models.MeasurementCategory
// @Failure 400 {object} map[string]string "编码或请求参数无效，编码错误返回 error_code=invalid_standard_code | Invalid code or request; code errors return error_code=invalid_standard_code"
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
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
	owner, ok := unitContext(c)
	if !ok {
		return
	}
	cat, err := h.svc.CreateCategory(&req, owner)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	cat.Localize(commoni18n.GetLang(c))
	c.JSON(http.StatusCreated, cat)
}

// @Summary 更新度量类别 | Update measurement category
// @Tags Standard
// @Param request body models.UpdateMeasurementCategoryRequest true "更新度量类别 | Update measurement category"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} models.MeasurementCategory
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
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
	owner, ok := unitContext(c)
	if !ok {
		return
	}
	cat, err := h.svc.UpdateCategory(id, owner, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	cat.Localize(commoni18n.GetLang(c))
	c.JSON(http.StatusOK, cat)
}

// @Summary 删除度量类别 | Delete measurement category
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 409 {object} map[string]string
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
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
	owner, ok := unitContext(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteCategory(id, owner); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// --- 计量单位 ---

// @Summary 获取计量单位列表 | List units
// @Tags Standard
// @Produce json
// @Success 200 {array} models.Unit
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.unit.read"]
// @Router /units [get]
// @Security BearerAuth
func (h *UnitHandler) ListUnits(c *gin.Context) {
	owner, ok := unitContext(c)
	if !ok {
		return
	}
	var categoryID *int64
	if catIDStr := c.Query("category_id"); catIDStr != "" {
		if id, err := strconv.ParseInt(catIDStr, 10, 64); err == nil {
			categoryID = &id
		}
	}
	units, err := h.svc.ListUnits(owner, categoryID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	for i := range units {
		units[i].Localize(commoni18n.GetLang(c))
	}
	c.JSON(http.StatusOK, units)
}

// @Summary 获取计量单位详情 | Get unit detail
// @Tags Standard
// @Produce json
// @Success 200 {object} models.Unit
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
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
	owner, ok := unitContext(c)
	if !ok {
		return
	}
	unit, err := h.svc.GetUnit(id, owner)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgUnitNotFound)})
		return
	}
	unit.Localize(commoni18n.GetLang(c))
	c.JSON(http.StatusOK, unit)
}

// @Summary 创建计量单位 | Create unit
// @Description 平台用户提交稳定编码与多语种译名；租户只创建自定义单位。| Platform users submit a stable code and runtime translations; tenant users create tenant-owned units.
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateUnitRequest true "计量单位 | Measurement unit"
// @Success 201 {object} models.Unit
// @Failure 400 {object} map[string]string "编码或译名无效 | Invalid code or translations"
// @Failure 409 {object} map[string]string "平台编码重复 | Duplicate platform code"
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
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
	owner, ok := unitContext(c)
	if !ok {
		return
	}
	unit, err := h.svc.CreateUnit(&req, owner)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	unit.Localize(commoni18n.GetLang(c))
	c.JSON(http.StatusCreated, unit)
}

// @Summary 更新计量单位 | Update unit
// @Tags Standard
// @Param request body models.UpdateUnitRequest true "更新计量单位 | Update unit"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} models.Unit
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
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
	owner, ok := unitContext(c)
	if !ok {
		return
	}
	unit, err := h.svc.UpdateUnit(id, owner, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	unit.Localize(commoni18n.GetLang(c))
	c.JSON(http.StatusOK, unit)
}

// @Summary 删除计量单位 | Delete unit
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 409 {object} map[string]string
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
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
	owner, ok := unitContext(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteUnit(id, owner); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// unitContext 只接受权威 AuthContext，不从 Tenant 缺省值推断平台身份。
func unitContext(c *gin.Context) (models.UnitContext, bool) {
	auth, exists := commonAuth.AuthContextFromGin(c)
	if exists && auth.Context.Type == "platform" && auth.Principal.Type == "user" {
		return models.UnitContext{Type: "platform"}, true
	}
	if exists && auth.Context.Type == "tenant" {
		if tenantID, ok := commonAuth.TenantIDFromGin(c); ok {
			return models.TenantUnitContext(tenantID), true
		}
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, sysi18n.MsgOperationFailed)})
	return models.UnitContext{}, false
}

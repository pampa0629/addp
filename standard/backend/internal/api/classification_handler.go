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

// ClassificationHandler 数据分类 + 数据分级 Handler
type ClassificationHandler struct {
	svc *service.ClassificationService
}

func NewClassificationHandler(svc *service.ClassificationService) *ClassificationHandler {
	return &ClassificationHandler{svc: svc}
}

// --- 数据分类 ---

// @Summary 获取数据分类列表 | List data classifications
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.classification.read"]
// @Router /classifications [get]
// @Security BearerAuth
func (h *ClassificationHandler) ListClassifications(c *gin.Context) {
	tenantID := getTenantID(c)
	list, err := h.svc.ListClassifications(tenantID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

// @Summary 创建数据分类 | Create data classification
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.classification.create"]
// @Router /classifications [post]
// @Security BearerAuth
func (h *ClassificationHandler) CreateClassification(c *gin.Context) {
	var req models.CreateClassificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	item, err := h.svc.CreateClassification(&req, tenantID, userID)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// @Summary 更新数据分类 | Update data classification
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.classification.update"]
// @Router /classifications/{id} [put]
// @Security BearerAuth
func (h *ClassificationHandler) UpdateClassification(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.UpdateClassificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	item, err := h.svc.UpdateClassification(id, tenantID, userID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 删除数据分类 | Delete data classification
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.classification.delete"]
// @Router /classifications/{id} [delete]
// @Security BearerAuth
func (h *ClassificationHandler) DeleteClassification(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteClassification(id, tenantID); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// --- 数据分级 ---

// @Summary 获取数据分级列表 | List data grading levels
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.classification.read"]
// @Router /grading-levels [get]
// @Security BearerAuth
func (h *ClassificationHandler) ListGradingLevels(c *gin.Context) {
	tenantID := getTenantID(c)
	levels, err := h.svc.ListGradingLevels(tenantID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, levels)
}

// @Summary 更新数据分级 | Update data grading level
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.classification.update"]
// @Router /grading-levels/{id} [put]
// @Security BearerAuth
func (h *ClassificationHandler) UpdateGradingLevel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.UpdateGradingLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.UpdateGradingLevel(id, tenantID, &req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgUpdateSuccess)})
}

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

type CodeSetHandler struct {
	codeSetService *service.CodeSetService
}

func NewCodeSetHandler(codeSetService *service.CodeSetService) *CodeSetHandler {
	return &CodeSetHandler{codeSetService: codeSetService}
}

// ListCodeSets 获取码值集列表
// @Summary 获取码值集列表 | List code sets
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.read"]
// @Router /code-sets [get]
// @Security BearerAuth
func (h *CodeSetHandler) ListCodeSets(c *gin.Context) {
	tenantID := c.GetInt64("tenant_id")

	keyword := c.Query("keyword")
	codeSetType := c.Query("type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	codeSets, total, err := h.codeSetService.ListCodeSets(tenantID, keyword, codeSetType, page, pageSize)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	c.JSON(http.StatusOK, gin.H{
		"data":        codeSets,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// CreateCodeSet 创建码值集
// @Summary 创建码值集 | Create code set
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.create"]
// @Router /code-sets [post]
// @Security BearerAuth
func (h *CodeSetHandler) CreateCodeSet(c *gin.Context) {
	tenantID := c.GetInt64("tenant_id")

	var req models.CreateCodeSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	codeSet, err := h.codeSetService.CreateCodeSet(tenantID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusCreated, codeSet)
}

// GetCodeSet 获取码值集详情
// @Summary 获取码值集详情 | Get code set detail
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.read"]
// @Router /code-sets/{id} [get]
// @Security BearerAuth
func (h *CodeSetHandler) GetCodeSet(c *gin.Context) {
	tenantID := c.GetInt64("tenant_id")
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	codeSet, err := h.codeSetService.GetCodeSet(id, tenantID)
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}

	c.JSON(http.StatusOK, codeSet)
}

// UpdateCodeSet 更新码值集
// @Summary 更新码值集 | Update code set
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id} [put]
// @Security BearerAuth
func (h *CodeSetHandler) UpdateCodeSet(c *gin.Context) {
	tenantID := c.GetInt64("tenant_id")
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	var req models.UpdateCodeSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	codeSet, err := h.codeSetService.UpdateCodeSet(id, tenantID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, codeSet)
}

// DeleteCodeSet 删除码值集
// @Summary 删除码值集 | Delete code set
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.delete"]
// @Router /code-sets/{id} [delete]
// @Security BearerAuth
func (h *CodeSetHandler) DeleteCodeSet(c *gin.Context) {
	tenantID := c.GetInt64("tenant_id")
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	if err := h.codeSetService.DeleteCodeSet(id, tenantID); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// GetCodeItems 获取码值项列表
// @Summary 获取码值项列表 | List code items
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.read"]
// @Router /code-sets/{id}/items [get]
// @Security BearerAuth
func (h *CodeSetHandler) GetCodeItems(c *gin.Context) {
	tenantID := c.GetInt64("tenant_id")
	codeSetID, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	items, err := h.codeSetService.GetCodeItems(codeSetID, tenantID)
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}

	c.JSON(http.StatusOK, items)
}

// CreateCodeItem 创建码值项
// @Summary 创建码值项 | Create code item
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id}/items [post]
// @Security BearerAuth
func (h *CodeSetHandler) CreateCodeItem(c *gin.Context) {
	tenantID := c.GetInt64("tenant_id")
	codeSetID, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	var req models.CreateCodeItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	item, err := h.codeSetService.CreateCodeItem(codeSetID, tenantID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusCreated, item)
}

// UpdateCodeItem 更新码值项
// @Summary 更新码值项 | Update code item
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id}/items/{iid} [put]
// @Security BearerAuth
func (h *CodeSetHandler) UpdateCodeItem(c *gin.Context) {
	tenantID := c.GetInt64("tenant_id")
	codeSetID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	itemID, _ := strconv.ParseInt(c.Param("iid"), 10, 64)

	var req models.UpdateCodeItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	item, err := h.codeSetService.UpdateCodeItem(codeSetID, itemID, tenantID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, item)
}

// DeleteCodeItem 删除码值项
// @Summary 删除码值项 | Delete code item
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id}/items/{iid} [delete]
// @Security BearerAuth
func (h *CodeSetHandler) DeleteCodeItem(c *gin.Context) {
	tenantID := c.GetInt64("tenant_id")
	codeSetID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	itemID, _ := strconv.ParseInt(c.Param("iid"), 10, 64)

	if err := h.codeSetService.DeleteCodeItem(codeSetID, itemID, tenantID); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

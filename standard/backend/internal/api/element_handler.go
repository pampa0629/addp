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

type ElementHandler struct {
	svc *service.ElementService
}

func NewElementHandler(svc *service.ElementService) *ElementHandler {
	return &ElementHandler{svc: svc}
}

// ListElements GET /api/model/elements
// @Summary 获取数据元列表 | List data elements
// @Tags Standard
// @Produce json
// @Success 200 {object} models.QualityRulesResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /elements [get]
// @Security BearerAuth
func (h *ElementHandler) ListElements(c *gin.Context) {
	tenantID := getTenantID(c)

	opts := repository.ListElementOptions{
		Status:  c.Query("status"),
		Keyword: c.Query("keyword"),
	}
	if domainIDStr := c.Query("domain_id"); domainIDStr != "" {
		if id, err := strconv.ParseInt(domainIDStr, 10, 64); err == nil {
			opts.DomainID = &id
		}
	}
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil {
			opts.Page = p
		}
	}
	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil {
			opts.PageSize = ps
		}
	}

	elements, total, err := h.svc.ListElements(tenantID, opts)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
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
	c.JSON(http.StatusOK, gin.H{"data": elements, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages})
}

// CreateElement POST /api/model/elements
// @Summary 创建数据元 | Create data element
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.create"]
// @Router /elements [post]
// @Security BearerAuth
func (h *ElementHandler) CreateElement(c *gin.Context) {
	var req models.CreateElementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	element, err := h.svc.CreateElement(&req, tenantID, userID)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, element)
}

// GetElement GET /api/model/elements/:id
// @Summary 获取数据元详情 | Get data element detail
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /elements/{id} [get]
// @Security BearerAuth
func (h *ElementHandler) GetElement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	element, err := h.svc.GetElement(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgElementNotFound)})
		return
	}
	c.JSON(http.StatusOK, element)
}

// UpdateElement PUT /api/model/elements/:id
// @Summary 更新数据元 | Update data element
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.update"]
// @Router /elements/{id} [put]
// @Security BearerAuth
func (h *ElementHandler) UpdateElement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	var req models.UpdateElementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	element, err := h.svc.UpdateElement(id, tenantID, userID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, element)
}

// DeleteElement DELETE /api/model/elements/:id
// @Summary 删除数据元 | Delete data element
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.delete"]
// @Router /elements/{id} [delete]
// @Security BearerAuth
func (h *ElementHandler) DeleteElement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteElement(id, tenantID); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// ApproveElement POST /api/model/elements/:id/approve
// @Summary 审批数据元 | Approve data element
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.approve"]
// @Router /elements/{id}/approve [post]
// @Security BearerAuth
func (h *ElementHandler) ApproveElement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	if err := h.svc.ApproveElement(id, tenantID, userID); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgApproveSuccess)})
}

// GetElementQualityRules GET /api/model/elements/:id/quality-rules
// @Summary 获取数据元质量规则 | Get data element quality rules
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /elements/{id}/quality-rules [get]
// @Security BearerAuth
func (h *ElementHandler) GetElementQualityRules(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	rules, err := h.svc.GetQualityRules(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgElementNotFound)})
		return
	}
	c.JSON(http.StatusOK, rules)
}

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

type GlossaryHandler struct {
	svc *service.GlossaryService
}

func NewGlossaryHandler(svc *service.GlossaryService) *GlossaryHandler {
	return &GlossaryHandler{svc: svc}
}

// ListGlossaries GET /api/model/glossaries
// @Summary 获取业务术语列表 | List glossaries
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /glossaries [get]
// @Security BearerAuth
func (h *GlossaryHandler) ListGlossaries(c *gin.Context) {
	tenantID := getTenantID(c)

	if elementIDStr := c.Query("element_id"); elementIDStr != "" {
		if elementID, err := strconv.ParseInt(elementIDStr, 10, 64); err == nil {
			glossaries, err := h.svc.GetGlossariesByElement(elementID, tenantID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, glossaries)
			return
		}
	}

	opts := repository.ListGlossaryOptions{
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

	glossaries, total, err := h.svc.ListGlossaries(tenantID, opts)
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
	c.JSON(http.StatusOK, gin.H{"data": glossaries, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages})
}

// CreateGlossary POST /api/model/glossaries
// @Summary 创建业务术语 | Create glossary
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /glossaries [post]
// @Security BearerAuth
func (h *GlossaryHandler) CreateGlossary(c *gin.Context) {
	var req models.CreateGlossaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	glossary, err := h.svc.CreateGlossary(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, glossary)
}

// GetGlossary GET /api/model/glossaries/:id
// @Summary 获取业务术语详情 | Get glossary detail
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /glossaries/{id} [get]
// @Security BearerAuth
func (h *GlossaryHandler) GetGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	glossary, err := h.svc.GetGlossary(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgGlossaryNotFound)})
		return
	}
	c.JSON(http.StatusOK, glossary)
}

// UpdateGlossary PUT /api/model/glossaries/:id
// @Summary 更新业务术语 | Update glossary
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /glossaries/{id} [put]
// @Security BearerAuth
func (h *GlossaryHandler) UpdateGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	var req models.UpdateGlossaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	glossary, err := h.svc.UpdateGlossary(id, tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, glossary)
}

// DeleteGlossary DELETE /api/model/glossaries/:id
// @Summary 删除业务术语 | Delete glossary
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /glossaries/{id} [delete]
// @Security BearerAuth
func (h *GlossaryHandler) DeleteGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteGlossary(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// ApproveGlossary POST /api/model/glossaries/:id/approve
// @Summary 审批业务术语 | Approve glossary
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /glossaries/{id}/approve [post]
// @Security BearerAuth
func (h *GlossaryHandler) ApproveGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	if err := h.svc.ApproveGlossary(id, tenantID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgApproveSuccess)})
}

// DeprecateGlossary POST /api/model/glossaries/:id/deprecate
// @Summary 废弃业务术语 | Deprecate glossary
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /glossaries/{id}/deprecate [post]
// @Security BearerAuth
func (h *GlossaryHandler) DeprecateGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	if err := h.svc.DeprecateGlossary(id, tenantID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeprecateSuccess)})
}

// GetElementMappings GET /glossaries/:id/elements
// @Summary 获取术语关联的数据元 | Get element mappings of glossary
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /glossaries/{id}/elements [get]
// @Security BearerAuth
func (h *GlossaryHandler) GetElementMappings(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	elements, err := h.svc.GetMappedElements(id, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, elements)
}

// SetElementMappings PUT /glossaries/:id/elements
// @Summary 设置术语关联的数据元 | Set element mappings of glossary
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /glossaries/{id}/elements [put]
// @Security BearerAuth
func (h *GlossaryHandler) SetElementMappings(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	var req struct {
		ElementIDs []int64 `json:"element_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.SetElementMappings(id, tenantID, req.ElementIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgUpdateSuccess)})
}

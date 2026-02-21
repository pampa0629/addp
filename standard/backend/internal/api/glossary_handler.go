package api

import (
	"net/http"
	"strconv"

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
func (h *GlossaryHandler) ListGlossaries(c *gin.Context) {
	tenantID := getTenantID(c)

	// 当有 element_id 参数时，直接返回该数据元关联的术语
	if elementIDStr := c.Query("element_id"); elementIDStr != "" {
		if elementID, err := strconv.ParseInt(elementIDStr, 10, 64); err == nil {
			glossaries, err := h.svc.GetGlossariesByElement(elementID, tenantID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"data": glossaries, "total": int64(len(glossaries))})
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
	c.JSON(http.StatusOK, gin.H{"data": glossaries, "total": total})
}

// CreateGlossary POST /api/model/glossaries
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
	c.JSON(http.StatusCreated, gin.H{"data": glossary})
}

// GetGlossary GET /api/model/glossaries/:id
func (h *GlossaryHandler) GetGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	glossary, err := h.svc.GetGlossary(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "glossary not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": glossary})
}

// UpdateGlossary PUT /api/model/glossaries/:id
func (h *GlossaryHandler) UpdateGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
	c.JSON(http.StatusOK, gin.H{"data": glossary})
}

// DeleteGlossary DELETE /api/model/glossaries/:id
func (h *GlossaryHandler) DeleteGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteGlossary(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ApproveGlossary POST /api/model/glossaries/:id/approve
func (h *GlossaryHandler) ApproveGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	if err := h.svc.ApproveGlossary(id, tenantID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "approved"})
}

// DeprecateGlossary POST /api/model/glossaries/:id/deprecate
func (h *GlossaryHandler) DeprecateGlossary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	if err := h.svc.DeprecateGlossary(id, tenantID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deprecated"})
}

// GetElementMappings GET /glossaries/:id/elements
func (h *GlossaryHandler) GetElementMappings(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	elements, err := h.svc.GetMappedElements(id, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": elements})
}

// SetElementMappings PUT /glossaries/:id/elements
func (h *GlossaryHandler) SetElementMappings(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

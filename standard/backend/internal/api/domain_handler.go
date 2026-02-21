package api

import (
	"net/http"
	"strconv"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

type DomainHandler struct {
	svc *service.DomainService
}

func NewDomainHandler(svc *service.DomainService) *DomainHandler {
	return &DomainHandler{svc: svc}
}

// ListDomains GET /api/model/domains
func (h *DomainHandler) ListDomains(c *gin.Context) {
	tenantID := getTenantID(c)
	tree, err := h.svc.ListDomainsAsTree(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tree})
}

// CreateDomain POST /api/model/domains
func (h *DomainHandler) CreateDomain(c *gin.Context) {
	var req models.CreateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	domain, err := h.svc.CreateDomain(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": domain})
}

// GetDomain GET /api/model/domains/:id
func (h *DomainHandler) GetDomain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	domain, err := h.svc.GetDomain(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": domain})
}

// UpdateDomain PUT /api/model/domains/:id
func (h *DomainHandler) UpdateDomain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req models.UpdateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	domain, err := h.svc.UpdateDomain(id, tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": domain})
}

// DeleteDomain DELETE /api/model/domains/:id
func (h *DomainHandler) DeleteDomain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteDomain(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

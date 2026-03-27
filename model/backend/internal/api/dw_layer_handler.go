package api

import (
	"net/http"
	"strconv"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type DWLayerHandler struct {
	svc *service.DWLayerService
}

func NewDWLayerHandler(svc *service.DWLayerService) *DWLayerHandler {
	return &DWLayerHandler{svc: svc}
}

// ListDWLayers GET /api/model/dw-layers
func (h *DWLayerHandler) ListDWLayers(c *gin.Context) {
	tenantID := getTenantID(c)

	layers, err := h.svc.ListDWLayers(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, layers)
}

// CreateDWLayer POST /api/model/dw-layers
func (h *DWLayerHandler) CreateDWLayer(c *gin.Context) {
	var req models.CreateDWLayerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	layer, err := h.svc.CreateDWLayer(&req, tenantID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, layer)
}

// GetDWLayer GET /api/model/dw-layers/:id
func (h *DWLayerHandler) GetDWLayer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	layer, err := h.svc.GetDWLayer(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "layer not found"})
		return
	}
	c.JSON(http.StatusOK, layer)
}

// UpdateDWLayer PUT /api/model/dw-layers/:id
func (h *DWLayerHandler) UpdateDWLayer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req models.UpdateDWLayerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	layer, err := h.svc.UpdateDWLayer(id, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, layer)
}

// DeleteDWLayer DELETE /api/model/dw-layers/:id
func (h *DWLayerHandler) DeleteDWLayer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteDWLayer(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

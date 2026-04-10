package api

import (
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	modeli18n "github.com/addp/model/i18n"
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
// @Summary ListDWLayers
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /listdwlayers [get]
// @Security BearerAuth
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
// @Summary CreateDWLayer
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /createdwlayer [get]
// @Security BearerAuth
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
// @Summary GetDWLayer
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /getdwlayer [get]
// @Security BearerAuth
func (h *DWLayerHandler) GetDWLayer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	layer, err := h.svc.GetDWLayer(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, modeli18n.MsgLayerNotFound)})
		return
	}
	c.JSON(http.StatusOK, layer)
}

// UpdateDWLayer PUT /api/model/dw-layers/:id
// @Summary UpdateDWLayer
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /updatedwlayer [get]
// @Security BearerAuth
func (h *DWLayerHandler) UpdateDWLayer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
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
// @Summary DeleteDWLayer
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /deletedwlayer [get]
// @Security BearerAuth
func (h *DWLayerHandler) DeleteDWLayer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteDWLayer(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

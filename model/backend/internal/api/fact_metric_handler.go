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

type FactMetricHandler struct {
	svc *service.FactMetricService
}

func NewFactMetricHandler(svc *service.FactMetricService) *FactMetricHandler {
	return &FactMetricHandler{svc: svc}
}

// ListMetrics GET /api/model/logical-tables/:id/metrics
// @Summary ListMetrics
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /listmetrics [get]
// @Security BearerAuth
func (h *FactMetricHandler) ListMetrics(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	mappings, err := h.svc.ListMetrics(tableID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, mappings)
}

// AddMetric POST /api/model/logical-tables/:id/metrics
// @Summary AddMetric
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /addmetric [get]
// @Security BearerAuth
func (h *FactMetricHandler) AddMetric(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	var req models.CreateFactMetricMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	mapping, err := h.svc.AddMetric(tableID, tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, mapping)
}

// RemoveMetric DELETE /api/model/logical-tables/:id/metrics/:mid
// @Summary RemoveMetric
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /removemetric [get]
// @Security BearerAuth
func (h *FactMetricHandler) RemoveMetric(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}
	mappingID, err := strconv.ParseInt(c.Param("mid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidMappingID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.RemoveMetric(mappingID, tableID, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "removed"})
}

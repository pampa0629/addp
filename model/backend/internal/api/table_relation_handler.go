package api

import (
	"net/http"
	"strconv"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type TableRelationHandler struct {
	svc *service.TableRelationService
}

func NewTableRelationHandler(svc *service.TableRelationService) *TableRelationHandler {
	return &TableRelationHandler{svc: svc}
}

// ListDimensionRelations GET /api/model/logical-tables/:id/dimension-relations
// @Summary ListDimensionRelations
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /listdimensionrelations [get]
// @Security BearerAuth
func (h *TableRelationHandler) ListDimensionRelations(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tenantID := getTenantID(c)
	relations, err := h.svc.ListDimensionRelations(tableID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, relations)
}

// AddDimensionRelation POST /api/model/logical-tables/:id/dimension-relations
// @Summary AddDimensionRelation
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /adddimensionrelation [get]
// @Security BearerAuth
func (h *TableRelationHandler) AddDimensionRelation(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req models.CreateTableRelationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	rel, err := h.svc.AddDimensionRelation(tableID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rel)
}

// RemoveDimensionRelation DELETE /api/model/logical-tables/:id/dimension-relations/:rid
// @Summary RemoveDimensionRelation
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /removedimensionrelation [get]
// @Security BearerAuth
func (h *TableRelationHandler) RemoveDimensionRelation(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	relationID, err := strconv.ParseInt(c.Param("rid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid relation id"})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.RemoveDimensionRelation(relationID, tableID, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "removed"})
}

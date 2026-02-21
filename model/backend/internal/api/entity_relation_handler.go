package api

import (
	"net/http"
	"strconv"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type EntityRelationHandler struct {
	svc *service.EntityRelationService
}

func NewEntityRelationHandler(svc *service.EntityRelationService) *EntityRelationHandler {
	return &EntityRelationHandler{svc: svc}
}

// CreateRelation POST /api/model/entity-relations
func (h *EntityRelationHandler) CreateRelation(c *gin.Context) {
	var req models.CreateEntityRelationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	relation, err := h.svc.Create(tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": relation})
}

// ListRelations GET /api/model/entity-relations?entity_id=123
func (h *EntityRelationHandler) ListRelations(c *gin.Context) {
	tenantID := getTenantID(c)
	entityIDStr := c.Query("entity_id")

	var relations []models.EntityRelation
	var err error

	if entityIDStr != "" {
		entityID, parseErr := strconv.ParseInt(entityIDStr, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid entity_id"})
			return
		}
		relations, err = h.svc.GetByEntityID(tenantID, entityID)
	} else {
		relations, err = h.svc.ListByTenantID(tenantID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": relations})
}

// GetRelation GET /api/model/entity-relations/:id
func (h *EntityRelationHandler) GetRelation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	relation, err := h.svc.GetByID(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "relation not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": relation})
}

// UpdateRelation PUT /api/model/entity-relations/:id
func (h *EntityRelationHandler) UpdateRelation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req models.UpdateEntityRelationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	relation, err := h.svc.Update(id, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": relation})
}

// DeleteRelation DELETE /api/model/entity-relations/:id
func (h *EntityRelationHandler) DeleteRelation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.Delete(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

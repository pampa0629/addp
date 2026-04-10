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

type EntityRelationHandler struct {
	svc *service.EntityRelationService
}

func NewEntityRelationHandler(svc *service.EntityRelationService) *EntityRelationHandler {
	return &EntityRelationHandler{svc: svc}
}

// CreateRelation POST /api/model/entity-relations
// @Summary CreateRelation
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /createrelation [get]
// @Security BearerAuth
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

	c.JSON(http.StatusCreated, relation)
}

// ListRelations GET /api/model/entity-relations?entity_id=123
// @Summary ListRelations
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /listrelations [get]
// @Security BearerAuth
func (h *EntityRelationHandler) ListRelations(c *gin.Context) {
	tenantID := getTenantID(c)
	entityIDStr := c.Query("entity_id")

	var relations []models.EntityRelation
	var err error

	if entityIDStr != "" {
		entityID, parseErr := strconv.ParseInt(entityIDStr, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidEntityIDQuery)})
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

	c.JSON(http.StatusOK, relations)
}

// GetRelation GET /api/model/entity-relations/:id
// @Summary GetRelation
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /getrelation [get]
// @Security BearerAuth
func (h *EntityRelationHandler) GetRelation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	relation, err := h.svc.GetByID(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, modeli18n.MsgRelationNotFound)})
		return
	}

	c.JSON(http.StatusOK, relation)
}

// UpdateRelation PUT /api/model/entity-relations/:id
// @Summary UpdateRelation
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /updaterelation [get]
// @Security BearerAuth
func (h *EntityRelationHandler) UpdateRelation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
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

	c.JSON(http.StatusOK, relation)
}

// DeleteRelation DELETE /api/model/entity-relations/:id
// @Summary DeleteRelation
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /deleterelation [get]
// @Security BearerAuth
func (h *EntityRelationHandler) DeleteRelation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.Delete(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

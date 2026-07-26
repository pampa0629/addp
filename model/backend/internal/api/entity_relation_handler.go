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
// @Summary 创建实体关系 | Create entity relation
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.CreateEntityRelationRequest true "创建请求 | Create request"
// @Success 201 {object} map[string]interface{} "已创建的关系 | Created relation"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity_relation.create"]
// @Router /entity-relations [post]
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
// @Summary 查询实体关系列表 | List entity relations
// @Tags Model
// @Produce json
// @Param entity_id query int false "实体ID过滤 | Filter by entity ID"
// @Success 200 {object} map[string]interface{} "关系列表 | Relation list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity_relation.read"]
// @Router /entity-relations [get]
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
// @Summary 获取实体关系详情 | Get entity relation details
// @Tags Model
// @Produce json
// @Param id path int true "关系ID | Relation ID"
// @Success 200 {object} map[string]interface{} "关系详情 | Relation details"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity_relation.read"]
// @Router /entity-relations/{id} [get]
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
// @Summary 更新实体关系 | Update entity relation
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "关系ID | Relation ID"
// @Param body body models.UpdateEntityRelationRequest true "更新请求 | Update request"
// @Success 200 {object} map[string]interface{} "已更新的关系 | Updated relation"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity_relation.update"]
// @Router /entity-relations/{id} [put]
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
// @Summary 删除实体关系 | Delete entity relation
// @Tags Model
// @Produce json
// @Param id path int true "关系ID | Relation ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity_relation.delete"]
// @Router /entity-relations/{id} [delete]
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

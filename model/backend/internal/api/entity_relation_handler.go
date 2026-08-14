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

// CreateRelation POST /api/v1/model/entity-relations
// @Summary 创建实体关系 | Create entity relation
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.CreateEntityRelationRequest true "创建请求 | Create request"
// @Success 201 {object} models.EntityRelation "已创建的关系 | Created relation"
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "关系实体不存在 | Referenced entity not found"
// @Failure 409 {object} models.ErrorResponse "关系重复、状态或自关联冲突 | Duplicate, state, or self-reference conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity_relation.create"]
// @Router /entity-relations [post]
// @Security BearerAuth
func (h *EntityRelationHandler) CreateRelation(c *gin.Context) {
	var req models.CreateEntityRelationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	relation, err := h.svc.Create(tenantID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, relation)
}

// ListRelations GET /api/v1/model/entity-relations?entity_id=123
// @Summary 查询实体关系列表 | List entity relations
// @Tags Model
// @Produce json
// @Param entity_id query int false "实体ID过滤 | Filter by entity ID"
// @Success 200 {array} models.EntityRelation "关系列表 | Relation list"
// @Failure 400 {object} models.ErrorResponse "实体 ID 过滤无效 | Invalid entity ID filter"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
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
		if parseErr != nil || entityID <= 0 {
			c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidEntityIDQuery), "invalid_entity_id"))
			return
		}
		relations, err = h.svc.GetByEntityID(tenantID, entityID)
	} else {
		relations, err = h.svc.ListByTenantID(tenantID)
	}

	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, relations)
}

// GetRelation GET /api/v1/model/entity-relations/:id
// @Summary 获取实体关系详情 | Get entity relation details
// @Tags Model
// @Produce json
// @Param id path int true "关系ID | Relation ID"
// @Success 200 {object} models.EntityRelation "关系详情 | Relation details"
// @Failure 400 {object} models.ErrorResponse "关系 ID 无效 | Invalid relation ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "实体关系不存在 | Entity relation not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity_relation.read"]
// @Router /entity-relations/{id} [get]
// @Security BearerAuth
func (h *EntityRelationHandler) GetRelation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	tenantID := getTenantID(c)
	relation, err := h.svc.GetByID(id, tenantID)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, relation)
}

// UpdateRelation PUT /api/v1/model/entity-relations/:id
// @Summary 更新实体关系 | Update entity relation
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "关系ID | Relation ID"
// @Param body body models.UpdateEntityRelationRequest true "更新请求 | Update request"
// @Success 200 {object} models.EntityRelation "已更新的关系 | Updated relation"
// @Failure 400 {object} models.ErrorResponse "请求或关系 ID 无效 | Invalid request or relation ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "实体关系或关系实体不存在 | Entity relation or referenced entity not found"
// @Failure 409 {object} models.ErrorResponse "关系重复或状态冲突 | Duplicate or relation state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity_relation.update"]
// @Router /entity-relations/{id} [put]
// @Security BearerAuth
func (h *EntityRelationHandler) UpdateRelation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	var req models.UpdateEntityRelationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	relation, err := h.svc.Update(id, tenantID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, relation)
}

// DeleteRelation DELETE /api/v1/model/entity-relations/:id
// @Summary 删除实体关系 | Delete entity relation
// @Tags Model
// @Produce json
// @Param id path int true "关系ID | Relation ID"
// @Param body body models.VersionRequest true "资源版本 | Resource version"
// @Success 200 {object} models.MessageResponse "删除成功 | Deleted successfully"
// @Failure 400 {object} models.ErrorResponse "关系 ID 无效 | Invalid relation ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "实体关系不存在 | Entity relation not found"
// @Failure 409 {object} models.ErrorResponse "关系实体状态冲突 | Relation entity state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity_relation.delete"]
// @Router /entity-relations/{id} [delete]
// @Security BearerAuth
func (h *EntityRelationHandler) DeleteRelation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.Delete(id, tenantID, req.Version); err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

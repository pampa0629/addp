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

const defaultProfessionalRelationLimit = 100
const maxProfessionalRelationLimit = 200

type ProfessionalRelationHandler struct {
	entityRelations *service.EntityRelationService
	tableRelations  *service.TableRelationService
}

func NewProfessionalRelationHandler(entityRelations *service.EntityRelationService, tableRelations *service.TableRelationService) *ProfessionalRelationHandler {
	return &ProfessionalRelationHandler{entityRelations: entityRelations, tableRelations: tableRelations}
}

func writeProfessionalRelations(c *gin.Context, response *models.ProfessionalRelationsResponse) {
	c.JSON(http.StatusOK, response)
}

func professionalRelationLimit(c *gin.Context) (int, bool) {
	value := c.Query("limit")
	if value == "" {
		return defaultProfessionalRelationLimit, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maxProfessionalRelationLimit {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgValidationFailed), "invalid_limit"))
		return 0, false
	}
	return limit, true
}

// GetEntityRelations 返回当前用户可读的一跳 Entity 专业关系图。
// @Summary 查询 Entity 专业关系图 | Get Entity professional relation graph
// @Description 返回当前 Entity 直接参与的 Model 权威关系；只使用当前 User AuthContext，不面向 Catalog Service Token 扩权 | Return Model-owned relations directly involving the Entity under the current User AuthContext; no Catalog service-token elevation
// @Tags Professional Relations
// @Produce json
// @Param id path int true "Entity ID"
// @Param limit query int false "最大边数量，默认100，最大200 | Maximum edges, default 100, maximum 200"
// @Success 200 {object} models.ProfessionalRelationsResponse
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "Entity 不存在 | Entity not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read","model.entity_relation.read"]
// @Router /entities/{id}/relations [get]
// @Security BearerAuth
func (h *ProfessionalRelationHandler) GetEntityRelations(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	limit, ok := professionalRelationLimit(c)
	if !ok {
		return
	}
	response, err := h.entityRelations.GetProfessionalRelations(getTenantID(c), id, limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeProfessionalRelations(c, response)
}

// GetLogicalTableRelations 返回当前用户可读的一跳 LogicalTable 专业关系图。
// @Summary 查询 LogicalTable 专业关系图 | Get LogicalTable professional relation graph
// @Description 返回 Model 权威的来源 Entity、表关系和指标引用；只使用当前 User AuthContext，且不调用 Standard 或 Catalog | Return Model-owned source Entity, table relations, and Metric references under the current User AuthContext without calling Standard or Catalog
// @Tags Professional Relations
// @Produce json
// @Param id path int true "LogicalTable ID"
// @Param limit query int false "最大边数量，默认100，最大200 | Maximum edges, default 100, maximum 200"
// @Success 200 {object} models.ProfessionalRelationsResponse
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "LogicalTable 不存在 | LogicalTable not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/relations [get]
// @Security BearerAuth
func (h *ProfessionalRelationHandler) GetLogicalTableRelations(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	limit, ok := professionalRelationLimit(c)
	if !ok {
		return
	}
	response, err := h.tableRelations.GetProfessionalRelations(getTenantID(c), id, limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeProfessionalRelations(c, response)
}

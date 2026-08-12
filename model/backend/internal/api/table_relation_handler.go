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

type TableRelationHandler struct {
	svc *service.TableRelationService
}

func NewTableRelationHandler(svc *service.TableRelationService) *TableRelationHandler {
	return &TableRelationHandler{svc: svc}
}

// ListDimensionRelations GET /api/model/logical-tables/:id/dimension-relations
// @Summary 查询维度关联列表 | List dimension relations
// @Tags Model
// @Produce json
// @Param id path int true "事实表ID | Fact table ID"
// @Success 200 {object} map[string]interface{} "维度关联列表 | Dimension relation list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/dimension-relations [get]
// @Security BearerAuth
func (h *TableRelationHandler) ListDimensionRelations(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	tenantID := getTenantID(c)
	relations, err := h.svc.ListDimensionRelations(tableID, tenantID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, relations)
}

// AddDimensionRelation POST /api/model/logical-tables/:id/dimension-relations
// @Summary 添加维度关联 | Add dimension relation
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "事实表ID | Fact table ID"
// @Param body body models.CreateTableRelationRequest true "创建请求 | Create request"
// @Success 201 {object} map[string]interface{} "已创建的关联 | Created relation"
// @Failure 400 {object} models.ErrorResponse "表或字段类型无效 | Invalid table or field type"
// @Failure 404 {object} models.ErrorResponse "逻辑表或字段不存在 | Logical table or field not found"
// @Failure 409 {object} models.ErrorResponse "表状态或关联冲突 | Table state or relation conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/dimension-relations [post]
// @Security BearerAuth
func (h *TableRelationHandler) AddDimensionRelation(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	var req models.CreateTableRelationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	tenantID := getTenantID(c)
	rel, err := h.svc.AddDimensionRelation(tableID, tenantID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, rel)
}

// RemoveDimensionRelation DELETE /api/model/logical-tables/:id/dimension-relations/:rid
// @Summary 删除维度关联 | Remove dimension relation
// @Tags Model
// @Produce json
// @Param id path int true "事实表ID | Fact table ID"
// @Param rid path int true "关联ID | Relation ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Removed successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/dimension-relations/{rid} [delete]
// @Security BearerAuth
func (h *TableRelationHandler) RemoveDimensionRelation(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	relationID, err := strconv.ParseInt(c.Param("rid"), 10, 64)
	if err != nil || relationID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidRelationID), "invalid_table_relation_id"))
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.RemoveDimensionRelation(relationID, tableID, tenantID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "removed"})
}

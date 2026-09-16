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

type ConceptMappingHandler struct {
	svc *service.ConceptMappingService
}

func NewConceptMappingHandler(svc *service.ConceptMappingService) *ConceptMappingHandler {
	return &ConceptMappingHandler{svc: svc}
}

// Get GET /api/v1/model/logical-tables/:id/concept-mappings
// @Summary 获取概念实现映射 | Get concept realization mappings
// @Description 返回逻辑表、字段和表关系到已审批业务实体模型的版本化映射。 | Return versioned mappings from a logical table, its fields, and table relations to the approved business entity model.
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Success 200 {object} models.ConceptMappingsResponse "概念实现映射 | Concept realization mappings"
// @Failure 400 {object} models.ErrorResponse "逻辑表 ID 无效 | Invalid logical table ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/concept-mappings [get]
// @Security BearerAuth
func (h *ConceptMappingHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	response, err := h.svc.Get(id, getTenantID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// Replace PUT /api/v1/model/logical-tables/:id/concept-mappings
// @Summary 替换概念实现映射 | Replace concept realization mappings
// @Description 在一个事务中完整替换逻辑表聚合的实体、属性和关系映射，并推进逻辑表版本。映射源必须已审批。 | Atomically replace entity, attribute, and relation mappings for the logical-table aggregate and advance its version. Mapping sources must be approved.
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param body body models.ReplaceConceptMappingsRequest true "完整映射集合 | Complete mapping set"
// @Success 200 {object} models.ConceptMappingsResponse "已保存的概念实现映射 | Saved concept realization mappings"
// @Failure 400 {object} models.ErrorResponse "映射请求无效 | Invalid mapping request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @Failure 409 {object} models.ErrorResponse "版本、状态或概念模型漂移冲突 | Version, state, or conceptual-model drift conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/concept-mappings [put]
// @Security BearerAuth
func (h *ConceptMappingHandler) Replace(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	var req models.ReplaceConceptMappingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	response, err := h.svc.Replace(id, getTenantID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

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
// @Summary 查询事实表关联指标列表 | List fact table metric mappings
// @Tags Model
// @Produce json
// @Param id path int true "事实表ID | Fact table ID"
// @Success 200 {object} map[string]interface{} "指标关联列表 | Metric mapping list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/metrics [get]
// @Security BearerAuth
func (h *FactMetricHandler) ListMetrics(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	tenantID := getTenantID(c)
	mappings, err := h.svc.ListMetrics(tableID, tenantID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, mappings)
}

// AddMetric POST /api/model/logical-tables/:id/metrics
// @Summary 添加事实表指标关联 | Add fact table metric mapping
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "事实表ID | Fact table ID"
// @Param body body models.CreateFactMetricMappingRequest true "创建请求 | Create request"
// @Success 201 {object} map[string]interface{} "已创建的关联 | Created mapping"
// @Failure 400 {object} models.ErrorResponse "请求的表类型无效 | Invalid table type"
// @Failure 404 {object} models.ErrorResponse "逻辑表或字段不存在 | Logical table or field not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态或指标关联冲突 | Logical table state or metric mapping conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/metrics [post]
// @Security BearerAuth
func (h *FactMetricHandler) AddMetric(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	var req models.CreateFactMetricMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	mapping, err := h.svc.AddMetric(tableID, tenantID, userID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapping)
}

// RemoveMetric DELETE /api/model/logical-tables/:id/metrics/:mid
// @Summary 删除事实表指标关联 | Remove fact table metric mapping
// @Tags Model
// @Produce json
// @Param id path int true "事实表ID | Fact table ID"
// @Param mid path int true "关联ID | Mapping ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Removed successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/metrics/{mid} [delete]
// @Security BearerAuth
func (h *FactMetricHandler) RemoveMetric(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	mappingID, err := strconv.ParseInt(c.Param("mid"), 10, 64)
	if err != nil || mappingID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidMappingID), "invalid_metric_mapping_id"))
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.RemoveMetric(mappingID, tableID, tenantID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "removed"})
}

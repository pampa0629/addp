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

// ListMetrics GET /api/v1/model/logical-tables/:id/metrics
// @Summary 查询事实表关联指标列表 | List fact table metric mappings
// @Tags Model
// @Produce json
// @Param id path int true "事实表ID | Fact table ID"
// @Success 200 {array} models.FactMetricMapping "指标关联列表 | Metric mapping list"
// @Failure 400 {object} models.ErrorResponse "事实表 ID 或表类型无效 | Invalid fact table ID or table type"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
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

// AddMetric POST /api/v1/model/logical-tables/:id/metrics
// @Summary 添加事实表指标关联 | Add fact table metric mapping
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "事实表ID | Fact table ID"
// @Param body body models.CreateFactMetricMappingRequest true "创建请求 | Create request"
// @Success 201 {object} models.FactMetricMutationResponse "已创建的关联 | Created mapping"
// @Failure 400 {object} models.ErrorResponse "请求的表类型无效 | Invalid table type"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表或字段不存在 | Logical table or field not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态或指标关联冲突 | Logical table state or metric mapping conflict"
// @Failure 503 {object} models.ErrorResponse "数据标准服务不可用 | Data Standard service unavailable"
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

// RemoveMetric DELETE /api/v1/model/logical-tables/:id/metrics/:mid
// @Summary 删除事实表指标关联 | Remove fact table metric mapping
// @Tags Model
// @Produce json
// @Param id path int true "事实表ID | Fact table ID"
// @Param mid path int true "关联ID | Mapping ID"
// @Param body body models.VersionRequest true "父资源版本 | Parent resource version"
// @Success 200 {object} models.VersionResponse "删除成功 | Removed successfully"
// @Failure 400 {object} models.ErrorResponse "事实表或关联 ID 无效 | Invalid fact table or mapping ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表或指标关联不存在 | Logical table or metric mapping not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态冲突 | Logical table state conflict"
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

	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	tenantID := getTenantID(c)
	response, err := h.svc.RemoveMetric(mappingID, tableID, tenantID, req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

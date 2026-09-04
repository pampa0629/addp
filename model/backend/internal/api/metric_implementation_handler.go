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

type MetricImplementationHandler struct {
	svc *service.MetricImplementationService
}

func NewMetricImplementationHandler(svc *service.MetricImplementationService) *MetricImplementationHandler {
	return &MetricImplementationHandler{svc: svc}
}
func metricImplementationPathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return 0, false
	}
	return id, true
}

// @Summary 查询事实表指标实现 | List fact table metric implementations
// @Tags Model
// @Produce json
// @Param id path int true "事实表 ID | Fact table ID"
// @Success 200 {array} models.MetricImplementation
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "事实表不存在 | Fact table not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/metric-implementations [get]
// @Security BearerAuth
func (h *MetricImplementationHandler) List(c *gin.Context) {
	tableID, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	items, err := h.svc.List(tableID, getTenantID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

// @Summary 创建指标实现 | Create metric implementation
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "事实表 ID | Fact table ID"
// @Param body body models.CreateMetricImplementationRequest true "完整指标实现与父版本 | Full metric implementation and parent version"
// @Success 201 {object} models.MetricImplementationMutationResponse
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "事实表、字段或指标定义修订不存在 | Fact table, field, or metric definition revision not found"
// @Failure 409 {object} models.ErrorResponse "版本、状态或唯一性冲突 | Version, state, or uniqueness conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/metric-implementations [post]
// @Security BearerAuth
func (h *MetricImplementationHandler) Create(c *gin.Context) {
	tableID, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	var req models.CreateMetricImplementationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	item, err := h.svc.Create(tableID, getTenantID(c), getUserID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// @Summary 更新指标实现 | Update metric implementation
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "事实表 ID | Fact table ID"
// @Param implementation_id path int true "指标实现 ID | Metric implementation ID"
// @Param body body models.UpdateMetricImplementationRequest true "完整指标实现与父版本 | Full metric implementation and parent version"
// @Success 200 {object} models.MetricImplementationMutationResponse
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "指标实现、字段或指标定义修订不存在 | Metric implementation, field, or metric definition revision not found"
// @Failure 409 {object} models.ErrorResponse "版本、状态或唯一性冲突 | Version, state, or uniqueness conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/metric-implementations/{implementation_id} [put]
// @Security BearerAuth
func (h *MetricImplementationHandler) Update(c *gin.Context) {
	tableID, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	implementationID, ok := metricImplementationPathID(c, "implementation_id")
	if !ok {
		return
	}
	var req models.UpdateMetricImplementationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	item, err := h.svc.Update(implementationID, tableID, getTenantID(c), getUserID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 删除指标实现 | Delete metric implementation
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "事实表 ID | Fact table ID"
// @Param implementation_id path int true "指标实现 ID | Metric implementation ID"
// @Param body body models.VersionRequest true "父资源版本 | Parent resource version"
// @Success 200 {object} models.VersionResponse
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "指标实现或事实表不存在 | Metric implementation or fact table not found"
// @Failure 409 {object} models.ErrorResponse "版本或状态冲突 | Version or state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/metric-implementations/{implementation_id} [delete]
// @Security BearerAuth
func (h *MetricImplementationHandler) Delete(c *gin.Context) {
	tableID, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	implementationID, ok := metricImplementationPathID(c, "implementation_id")
	if !ok {
		return
	}
	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.Delete(implementationID, tableID, getTenantID(c), req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

package api

import (
	commoni18n "github.com/addp/common/middleware/i18n"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
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

// @Summary 查询指标实现 | List metric implementations
// @Tags Model
// @Produce json
// @Param fact_table_id query int false "来源事实表 | Source fact table"
// @Success 200 {array} models.MetricImplementation
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "资源不存在 | Resource not found"
// @Failure 409 {object} models.ErrorResponse "版本或依赖冲突 | Version or dependency conflict"
// @Failure 503 {object} models.ErrorResponse "依赖不可用 | Dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.metric_implementation.read"]
// @Router /metric-implementations [get]
// @Security BearerAuth
func (h *MetricImplementationHandler) List(c *gin.Context) {
	var factID int64
	if raw := c.Query("fact_table_id"); raw != "" {
		var err error
		factID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || factID <= 0 {
			c.JSON(400, invalidParamsResponse(c))
			return
		}
	}
	result, err := h.svc.List(factID, getTenantID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(200, result)
}

// @Summary 读取指标实现及修订 | Read metric implementation and revisions
// @Tags Model
// @Produce json
// @Param id path int true "指标实现 ID | Metric implementation ID"
// @Success 200 {object} models.MetricImplementation
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "资源不存在 | Resource not found"
// @Failure 409 {object} models.ErrorResponse "版本或依赖冲突 | Version or dependency conflict"
// @Failure 503 {object} models.ErrorResponse "依赖不可用 | Dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.metric_implementation.read"]
// @Router /metric-implementations/{id} [get]
// @Security BearerAuth
func (h *MetricImplementationHandler) Get(c *gin.Context) {
	id, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.Get(id, getTenantID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(200, result)
}

// @Summary 创建独立指标实现 | Create independent metric implementation
// @Tags Model
// @Produce json
// @Accept json
// @Param body body models.CreateMetricImplementationRequest true "请求 | Request"
// @Success 201 {object} models.MetricImplementation
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "资源不存在 | Resource not found"
// @Failure 409 {object} models.ErrorResponse "版本或依赖冲突 | Version or dependency conflict"
// @Failure 503 {object} models.ErrorResponse "依赖不可用 | Dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.metric_implementation.create"]
// @Router /metric-implementations [post]
// @Security BearerAuth
func (h *MetricImplementationHandler) Create(c *gin.Context) {
	var req models.CreateMetricImplementationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.Create(c.Request.Context(), getTenantID(c), getUserID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(201, result)
}

// @Summary 保存指标实现草稿 | Save metric implementation draft
// @Description 保存数据库无关的结构化计算契约；来源结构来自 Meta，执行引擎必须声明分析计算能力。 | Saves a database-independent computation contract using Meta source facts and a certified analytical engine.
// @Description 可选 subject_label 必须引用主体维度的 string 字段，向结果追加当前主体及比较方名称，保留原始标识与计算值。 | Optional subject_label references a string field in the subject dimension and adds current subject and comparison labels while preserving identities and metric values.
// @Tags Model
// @Produce json
// @Accept json
// @Param body body models.SaveMetricImplementationRevisionRequest true "请求 | Request"
// @Param id path int true "指标实现 ID | Metric implementation ID"
// @Success 200 {object} models.MetricImplementation
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "资源不存在 | Resource not found"
// @Failure 409 {object} models.ErrorResponse "版本或依赖冲突 | Version or dependency conflict"
// @Failure 503 {object} models.ErrorResponse "依赖不可用 | Dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.metric_implementation.update"]
// @Router /metric-implementations/{id}/draft [put]
// @Security BearerAuth
func (h *MetricImplementationHandler) SaveDraft(c *gin.Context) {
	id, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	var req models.SaveMetricImplementationRevisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.SaveDraft(c.Request.Context(), id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(200, result)
}

// @Summary 发布指标实现修订 | Publish metric implementation revision
// @Description 重新校验来源与方言依赖后冻结发布；依赖变化须重新保存草稿。 | Revalidates source and dialect dependencies before publication; changed dependencies require saving the draft again.
// @Tags Model
// @Produce json
// @Accept json
// @Param body body models.MetricImplementationVersionRequest true "请求 | Request"
// @Param id path int true "指标实现 ID | Metric implementation ID"
// @Param revision_id path int true "修订 ID | Revision ID"
// @Success 200 {object} models.MetricImplementation
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "资源不存在 | Resource not found"
// @Failure 409 {object} models.ErrorResponse "版本或依赖冲突 | Version or dependency conflict"
// @Failure 503 {object} models.ErrorResponse "依赖不可用 | Dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.metric_implementation.publish"]
// @Router /metric-implementations/{id}/revisions/{revision_id}/publish [post]
// @Security BearerAuth
func (h *MetricImplementationHandler) Publish(c *gin.Context) {
	id, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	revisionID, ok := metricImplementationPathID(c, "revision_id")
	if !ok {
		return
	}
	var req models.MetricImplementationVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.ChangeRevisionState(c.Request.Context(), id, revisionID, getTenantID(c), getUserID(c), req.Version, true)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(200, result)
}

// @Summary 撤回指标实现修订 | Withdraw metric implementation revision
// @Tags Model
// @Produce json
// @Accept json
// @Param body body models.MetricImplementationVersionRequest true "请求 | Request"
// @Param id path int true "指标实现 ID | Metric implementation ID"
// @Param revision_id path int true "修订 ID | Revision ID"
// @Success 200 {object} models.MetricImplementation
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "资源不存在 | Resource not found"
// @Failure 409 {object} models.ErrorResponse "版本或依赖冲突 | Version or dependency conflict"
// @Failure 503 {object} models.ErrorResponse "依赖不可用 | Dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.metric_implementation.offline"]
// @Router /metric-implementations/{id}/revisions/{revision_id}/withdraw [post]
// @Security BearerAuth
func (h *MetricImplementationHandler) Withdraw(c *gin.Context) {
	id, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	revisionID, ok := metricImplementationPathID(c, "revision_id")
	if !ok {
		return
	}
	var req models.MetricImplementationVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.ChangeRevisionState(c.Request.Context(), id, revisionID, getTenantID(c), getUserID(c), req.Version, false)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(200, result)
}

// @Summary 删除未发布指标实现 | Delete unpublished metric implementation
// @Tags Model
// @Produce json
// @Accept json
// @Param body body models.MetricImplementationVersionRequest true "请求 | Request"
// @Param id path int true "指标实现 ID | Metric implementation ID"
// @Success 204 "已删除 | Deleted"
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "资源不存在 | Resource not found"
// @Failure 409 {object} models.ErrorResponse "版本或依赖冲突 | Version or dependency conflict"
// @Failure 503 {object} models.ErrorResponse "依赖不可用 | Dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.metric_implementation.delete"]
// @Router /metric-implementations/{id} [delete]
// @Security BearerAuth
func (h *MetricImplementationHandler) Delete(c *gin.Context) {
	id, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	var req models.MetricImplementationVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	err := h.svc.Delete(id, getTenantID(c), getUserID(c), req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(204)
}

// @Accept json
// @Param body body models.MetricPlanRequest true "计划参数 | Plan parameters"
// @Summary 读取已发布指标计算计划 | Read published metric computation plan
// @Description result_kind=details 读取同修订去重明细；省略读取汇总。 | result_kind=details selects distinct members of the same revision; omission selects summary.
// @Tags Model
// @Produce json
// @Param id path int true "指标实现 ID | Metric implementation ID"
// @Param revision_id path int true "修订 ID | Revision ID"
// @Success 200 {object} service.MetricCompiledPlan
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Unauthorized"
// @Failure 403 {object} models.ErrorResponse "无权限 | Forbidden"
// @Failure 404 {object} models.ErrorResponse "资源不存在 | Resource not found"
// @Failure 409 {object} models.ErrorResponse "版本或依赖冲突 | Version or dependency conflict"
// @Failure 503 {object} models.ErrorResponse "依赖不可用 | Dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.metric_implementation.read"]
// @Router /metric-implementations/{id}/revisions/{revision_id}/plan [post]
// @Security BearerAuth
func (h *MetricImplementationHandler) Plan(c *gin.Context) {
	id, ok := metricImplementationPathID(c, "id")
	if !ok {
		return
	}
	revisionID, ok := metricImplementationPathID(c, "revision_id")
	if !ok {
		return
	}
	var req models.MetricPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.PublishedPlan(c.Request.Context(), id, revisionID, getTenantID(c), req.Input, req.ResultKind)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(200, result)
}

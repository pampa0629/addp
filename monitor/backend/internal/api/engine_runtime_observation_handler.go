package api

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// EngineRuntime godoc
// @Summary 读取搜索与计算运行指标 | Read search and compute runtime metrics
// @Description 读取当前 Meilisearch 或 GeoPython 的固定指标。趋势需要实例身份、指标和完整时段，最长七天 | Read fixed metrics for current Meilisearch or GeoPython. Trends require the current identity, one metric and a complete range up to seven days
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param source query string true "来源类型 | Source kind" Enums(meilisearch, geopython)
// @Param id query string false "当前来源 UUID，读取趋势时必填 | Current source UUID, required for trends"
// @Param metric query string false "来源的固定指标键，读取趋势时必填 | Fixed source metric key, required for trends"
// @Param start query string false "RFC3339 趋势开始时间 | RFC3339 trend start"
// @Param end query string false "RFC3339 趋势结束时间 | RFC3339 trend end"
// @Success 200 {object} service.EngineRuntimeObservation
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.resource_observation.read","platform.engine_deployment.read"]
// @Router /platform/engine_runtime_observations [get]
func (h *ResourceObservationHandler) EngineRuntime(c *gin.Context) {
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	source := q.Get("source")
	valid := err == nil && len(c.Request.URL.RawQuery) <= 4096 && c.Request.ContentLength == 0 && len(c.Request.TransferEncoding) == 0 &&
		(len(q) == 1 || len(q) == 5) && (source == "meilisearch" || source == "geopython")
	for key, values := range q {
		switch key {
		case "source", "id", "metric", "start", "end":
			valid = valid && len(values) == 1 && values[0] != ""
		default:
			valid = false
		}
	}
	var start, end time.Time
	id, metric := q.Get("id"), q.Get("metric")
	if len(q) == 5 {
		parsed, parseErr := uuid.Parse(id)
		valid = valid && parseErr == nil && parsed != uuid.Nil && parsed.String() == id
		start, err = time.Parse(time.RFC3339, q.Get("start"))
		valid = valid && err == nil
		end, err = time.Parse(time.RFC3339, q.Get("end"))
		valid = valid && err == nil
	}
	if !valid {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	var result service.EngineRuntimeObservation
	result, err = h.service.EngineRuntime(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), source, id, metric, start, end)
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

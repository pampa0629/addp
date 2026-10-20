package api

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type HTTPObservationHandler struct {
	service *service.ResourceObservationService
}

func (h HTTPObservationHandler) read(c *gin.Context, grouped, trend bool) {
	c.Header("Cache-Control", "no-store")
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(c.Request.URL.RawQuery) > 4096 || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	for key, values := range q {
		if len(values) != 1 || (values[0] == "" && key != "route") || (key != "instance_ids" && key != "window_seconds" && key != "route" && key != "method" && (!grouped || key != "sort") && (!trend || (key != "start" && key != "end"))) {
			resourceError(c, resourcequery.ErrInvalid)
			return
		}
	}
	ids, err := models.ParseRuntimeInstanceIDs(q.Get("instance_ids"))
	if err != nil {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	window := 60
	if q.Has("window_seconds") {
		window, err = strconv.Atoi(q.Get("window_seconds"))
		if err != nil {
			resourceError(c, resourcequery.ErrInvalid)
			return
		}
	}
	request := service.HTTPObservationRequest{IDs: ids, WindowSeconds: window, Grouped: grouped, Trend: trend, Method: q.Get("method")}
	request.Sort = q.Get("sort")
	if q.Has("route") {
		route := q.Get("route")
		request.Route = &route
	}
	if trend {
		request.Start, err = time.Parse(time.RFC3339, q.Get("start"))
		if err != nil {
			resourceError(c, resourcequery.ErrInvalid)
			return
		}
		request.End, err = time.Parse(time.RFC3339, q.Get("end"))
		if err != nil {
			resourceError(c, resourcequery.ErrInvalid)
			return
		}
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	value, err := h.service.HTTP(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), targetToken(c), request)
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

// Summaries godoc
// @Summary 读取平台 HTTP 实例质量 | Read platform HTTP instance quality
// @Description 仅非委托 Platform User；System 逐请求裁决实例读取。Gateway 与 Backend 分别解释，禁止相加为全局QPS。完整窗口、真实无请求、缺样及采集失败分别表达 | Non-delegated Platform User with current System instance authorization. Ingress and Backend rates are separate; empty traffic is distinct from missing samples or collection failure
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param instance_ids query string true "System 当前实例 ID，逗号分隔；七指标乘实例数计入预算 | Current System instance IDs, comma-separated; seven metrics per instance count against budget"
// @Param window_seconds query int false "固定统计窗口 | Fixed request window" Enums(60,300,900) default(60)
// @Success 200 {object} service.HTTPObservationResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.http_observation.read"]
// @Router /platform/http_observations [get]
func (h HTTPObservationHandler) Summaries(c *gin.Context) { h.read(c, false, false) }

// Interfaces godoc
// @Summary 读取实例接口质量与慢接口 | Read instance interface quality and slow routes
// @Description 仅已观测且窗口完整的框架模板，显式TOP10按平均耗时或QPS排名；更紧预算相应减小排名数。六指标乘接口数计入预算，不读取原始URL | Explicit TOP10 eligible observed templates by mean latency or QPS; tighter budgets reduce the ranking size. Six metrics per route count against budget; no raw URLs
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param instance_ids query string true "恰好一个当前实例 ID | Exactly one current instance ID"
// @Param window_seconds query int false "固定统计窗口 | Fixed request window" Enums(60,300,900) default(60)
// @Param sort query string false "排名依据 | Rank by" Enums(mean,qps) default(mean)
// @Success 200 {object} service.HTTPObservationResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.http_observation.read"]
// @Router /platform/http_interfaces [get]
func (h HTTPObservationHandler) Interfaces(c *gin.Context) { h.read(c, true, false) }

// Trend godoc
// @Summary 读取实例或接口质量趋势 | Read instance or route quality trends
// @Description route/method 必须同时提供；省略时读取整个实例，活动请求数仅属于实例，窗口固定，复用受限时间范围和采样预算 | Supply route and method together, or omit both for the process. Active requests are process-scoped; fixed windows and bounded range/point budgets apply
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param instance_ids query string true "恰好一个当前实例 ID | Exactly one current instance ID"
// @Param window_seconds query int false "固定统计窗口 | Fixed request window" Enums(60,300,900) default(60)
// @Param route query string false "框架模板；空值表示未匹配路由 | Framework template; empty selects unmatched routes"
// @Param method query string false "固定 HTTP 方法 | Fixed HTTP method"
// @Param start query string true "RFC3339 整秒起点 | Whole-second RFC3339 start"
// @Param end query string true "RFC3339 整秒终点 | Whole-second RFC3339 end"
// @Success 200 {object} service.HTTPObservationResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.http_observation.read"]
// @Router /platform/http_trends [get]
func (h HTTPObservationHandler) Trend(c *gin.Context) { h.read(c, false, true) }

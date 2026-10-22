package api

import (
	"context"
	"errors"
	"github.com/addp/common/client"
	auth "github.com/addp/common/middleware/auth"
	i18n "github.com/addp/common/middleware/i18n"
	monitori18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/service"
	"github.com/addp/monitor/internal/tracequery"
	"github.com/gin-gonic/gin"
	"net/url"
	"strconv"
	"time"
)

type TraceHandler struct{ service *service.TraceService }

func traceError(c *gin.Context, e error) {
	status, code, key := 503, "trace_backend_unavailable", monitori18n.MsgTraceUnavailable
	switch {
	case errors.Is(e, tracequery.ErrInvalid):
		status, code, key = 400, "invalid_trace_query", monitori18n.MsgTraceInvalid
	case errors.Is(e, tracequery.ErrBudget):
		status, code, key = 422, "trace_query_budget_exceeded", monitori18n.MsgTraceBudget
	case errors.Is(e, tracequery.ErrBusy):
		status, code, key = 429, "trace_query_busy", monitori18n.MsgTraceBusy
	case errors.Is(e, tracequery.ErrDenied), errors.Is(e, tracequery.ErrNotFound):
		status, code, key = 404, "trace_not_found", monitori18n.MsgTraceNotFound
	case errors.Is(e, context.DeadlineExceeded):
		status, code, key = 504, "trace_query_timeout", monitori18n.MsgTraceTimeout
	default:
		if status, ok := client.SystemAPIStatusCode(e); ok && (status == 401 || status == 403 || status == 404) {
			targetError(c, e)
			return
		}
	}
	c.JSON(status, gin.H{"error": i18n.T(c, key), "error_code": code})
}
func (h TraceHandler) read(c *gin.Context, detail bool) {
	c.Header("Cache-Control", "no-store")
	q, e := url.ParseQuery(c.Request.URL.RawQuery)
	if e != nil || len(c.Request.URL.RawQuery) > 4096 || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		traceError(c, tracequery.ErrInvalid)
		return
	}
	for k, v := range q {
		if len(v) != 1 || v[0] == "" || (k != "instance_id" && k != "start" && k != "end" && k != "limit") {
			traceError(c, tracequery.ErrInvalid)
			return
		}
	}
	id, e := strconv.ParseUint(q.Get("instance_id"), 10, 32)
	if e != nil || id == 0 || strconv.FormatUint(id, 10) != q.Get("instance_id") {
		traceError(c, tracequery.ErrInvalid)
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	input := service.TraceQuery{InstanceID: uint(id), Start: now.Add(-15 * time.Minute), End: now, Limit: 20}
	if q.Has("start") || q.Has("end") {
		input.Start, e = time.Parse(time.RFC3339, q.Get("start"))
		if e != nil {
			traceError(c, tracequery.ErrInvalid)
			return
		}
		input.End, e = time.Parse(time.RFC3339, q.Get("end"))
		if e != nil {
			traceError(c, tracequery.ErrInvalid)
			return
		}
	}
	if q.Has("limit") {
		input.Limit, e = strconv.Atoi(q.Get("limit"))
		if e != nil || strconv.Itoa(input.Limit) != q.Get("limit") {
			traceError(c, tracequery.ErrInvalid)
			return
		}
	}
	if detail {
		input.ID = c.Param("trace_id")
		input.Limit = 1
		if q.Has("limit") {
			traceError(c, tracequery.ErrInvalid)
			return
		}
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(401)
		return
	}
	value, e := h.service.Query(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), targetToken(c), input)
	if e != nil {
		traceError(c, e)
		return
	}
	c.JSON(200, value)
}

// Search godoc
// @Summary 读取平台实例调用链 | Read platform instance traces
// @Description 非委托Platform User；System逐请求裁决所有参与实例及因果Link目标；仅有界固定查询，采样结果不代表全量或完整调用关系 | Non-delegated Platform User; System authorizes every participant and causal Link target; bounded sampled evidence does not represent complete traffic
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param instance_id query int true "System实例记录ID | System instance record ID"
// @Param start query string false "UTC起点，与end成对，最长1小时 | UTC start paired with end, at most one hour"
// @Param end query string false "UTC终点 | UTC end"
// @Param limit query int false "最多100条且总1000跨度预算 | At most 100 traces and 1000 total spans" default(20)
// @Success 200 {object} service.TraceResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.trace.read"]
// @Router /platform/traces [get]
func (h TraceHandler) Search(c *gin.Context) { h.read(c, false) }

// Detail godoc
// @Summary 读取调用链详情与观测调用关系 | Read trace details and observed call relationships
// @Description 所有跨度及Link目标重核实例读取权；独立执行阶段与因果Link不生成HTTP调用边，关联ID不授予日志正文权限 | Reauthorize spans and Link targets; execution stages and causal Links do not create HTTP edges or grant log access
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param trace_id path string true "规范Trace ID | Canonical trace ID"
// @Param instance_id query int true "System实例记录ID | System instance record ID"
// @Param start query string false "UTC起点，与end成对 | UTC start paired with end"
// @Param end query string false "UTC终点 | UTC end"
// @Success 200 {object} service.TraceResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.trace.read"]
// @Router /platform/traces/{trace_id} [get]
func (h TraceHandler) Detail(c *gin.Context) { h.read(c, true) }

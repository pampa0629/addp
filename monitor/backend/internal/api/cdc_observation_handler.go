package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

// CDCInstant godoc
// @Summary 读取 CDC 组件指标 | Read CDC component observations
// @Description 当前 Infra Redpanda 与 Kafka Connect 各至多一项，固定目录、共享预算及完成时身份重核；发现不可用与空清单分开，不读取 Connector 配置或业务数据 | At most one current Infra Redpanda and Kafka Connect component each; fixed catalogs, shared budget and completion-time identity recheck; distinguish unavailable discovery from an empty inventory; no connector configuration or business data
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.CDCResourceResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.resource_observation.read"]
// @Router /platform/cdc_resource_observations [get]
func (h *ResourceObservationHandler) CDCInstant(c *gin.Context) { h.cdcResources(c, false) }

// CDCTrend godoc
// @Summary 读取当前 CDC 组件运行段趋势 | Read current CDC component trends
// @Description 仅当前组件容器运行段，固定类型指标最多十二项、最长七天；缺样不补零，不拼接旧容器，源端延迟不是链路端到端延迟 | Only the current component container segment; at most twelve fixed per-type metrics and seven days; no missing-sample zero fill or old-container stitching; source delay is not end-to-end latency
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param component_id query string true "当前组件 UUID | Current component UUID"
// @Param metrics query string true "逗号分隔的固定指标键 | Comma-separated fixed metric keys"
// @Param start query string true "RFC3339 开始时间 | RFC3339 start time"
// @Param end query string true "RFC3339 结束时间 | RFC3339 end time"
// @Success 200 {object} service.CDCResourceResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.resource_observation.read"]
// @Router /platform/cdc_resource_trends [get]
func (h *ResourceObservationHandler) CDCTrend(c *gin.Context) { h.cdcResources(c, true) }
func (h *ResourceObservationHandler) cdcResources(c *gin.Context, trend bool) {
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	valid := err == nil && len(c.Request.URL.RawQuery) <= 4096 && c.Request.ContentLength == 0 && len(c.Request.TransferEncoding) == 0
	if !trend {
		valid = valid && len(q) == 0
	} else {
		valid = valid && len(q) == 4
	}
	for k, v := range q {
		if !trend || (k != "component_id" && k != "metrics" && k != "start" && k != "end") || len(v) != 1 || v[0] == "" {
			valid = false
		}
	}
	var start, end time.Time
	if trend {
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
	var value service.CDCResourceResponse
	value, err = h.service.CDCResources(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), q.Get("component_id"), strings.Split(q.Get("metrics"), ","), start, end, trend)
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

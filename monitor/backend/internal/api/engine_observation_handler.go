package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/gin-gonic/gin"
)

// EngineInstant godoc
// @Summary 读取 PostgreSQL 即时指标 | Read current PostgreSQL metrics
// @Description 仅当前已纳管且有效绑定主机的 Infra/Business PostgreSQL；固定十二项指标，逐请求用当前 User 向 System 核验并在返回前重核；不读取业务数据或凭据 | Only admitted and currently host-bound Infra/Business PostgreSQL; twelve fixed metrics, current User authorization and completion-time recheck; no business data or credentials
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param deployment_id query string true "部署 UUID | Deployment UUID"
// @Param metrics query string true "逗号分隔的固定 PostgreSQL 指标键，最多十二项 | Up to twelve comma-separated fixed PostgreSQL metric keys"
// @Success 200 {object} service.EngineResourceResponse
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
// @x-addp-required-permissions ["monitor.resource_observation.read"]
// @Router /platform/engine_resource_observations [get]
func (h *ResourceObservationHandler) EngineInstant(c *gin.Context) { h.engineResources(c,false,false) }

// EngineSummaries godoc
// @Summary 读取当前页 PostgreSQL 资源摘要 | Read current-page PostgreSQL summaries
// @Description 当前页总连接、活跃连接、每秒提交和总容量；数量乘四计入同一预算，整批超限拒绝，System 用当前 User 逐一裁决 | Current-page total and active connections, commits/second and total size; four metrics per deployment under one budget, reject the whole over-budget batch; System authorizes each current User read
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param deployment_ids query string true "逗号分隔的1至100个不重复部署 UUID，受当前预算约束 | 1 to 100 distinct comma-separated deployment UUIDs, subject to current budget"
// @Success 200 {object} service.EngineResourceResponse
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
// @x-addp-required-permissions ["monitor.resource_observation.read"]
// @Router /platform/engine_resource_summaries [get]
func (h *ResourceObservationHandler) EngineSummaries(c *gin.Context) { h.engineResources(c,false,true) }

// EngineTrend godoc
// @Summary 读取当前 PostgreSQL 容器段趋势 | Read current PostgreSQL container trends
// @Description 最长七天；排除本容器启动前的时间，不拼接旧容器；一分钟速率在缺样、重启或统计重置处留空，固定查询目录和整批预算 | At most seven days; exclude time before the current container start, never join an old container; one-minute rates leave gaps across missing samples, restarts or statistics resets; fixed catalog and bounded query budget
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param deployment_id query string true "部署 UUID | Deployment UUID"
// @Param metrics query string true "逗号分隔的固定 PostgreSQL 指标键，最多十二项 | Up to twelve fixed PostgreSQL metric keys"
// @Param start query string true "RFC3339 开始时间 | RFC3339 start time"
// @Param end query string true "RFC3339 结束时间 | RFC3339 end time"
// @Success 200 {object} service.EngineResourceResponse
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
// @x-addp-required-permissions ["monitor.resource_observation.read"]
// @Router /platform/engine_resource_trends [get]
func (h *ResourceObservationHandler) EngineTrend(c *gin.Context) { h.engineResources(c,true,false) }

func (h *ResourceObservationHandler) engineResources(c *gin.Context,trend,summary bool) {
	q,err:=url.ParseQuery(c.Request.URL.RawQuery)
	valid:=err==nil && len(c.Request.URL.RawQuery)<=4096 && c.Request.ContentLength==0 && len(c.Request.TransferEncoding)==0
	if summary { valid=valid && q.Get("deployment_ids")!="" } else { valid=valid && q.Get("deployment_id")!="" && q.Get("metrics")!="" }
	for key,values:=range q {
		if len(values)!=1 || values[0]=="" || (summary && key!="deployment_ids") || (!summary && key!="deployment_id" && key!="metrics" && (!trend || (key!="start" && key!="end"))) { valid=false }
	}
	var ids,keys []string; var start,end time.Time
	if summary { ids=strings.Split(q.Get("deployment_ids"),",") } else { ids=[]string{q.Get("deployment_id")}; keys=strings.Split(q.Get("metrics"),",") }
	if trend { start,err=time.Parse(time.RFC3339,q.Get("start")); if err!=nil { valid=false }; end,err=time.Parse(time.RFC3339,q.Get("end")); if err!=nil { valid=false } }
	if !valid { resourceError(c,resourcequery.ErrInvalid); return }
	principal,ok:=auth.PrincipalIDFromGin(c); if !ok { c.Status(http.StatusUnauthorized); return }
	value,err:=h.service.EngineResources(c.Request.Context(),strconv.FormatUint(uint64(principal),10),targetToken(c),ids,keys,start,end,trend,summary)
	if err!=nil { resourceError(c,err); return }; c.JSON(http.StatusOK,value)
}

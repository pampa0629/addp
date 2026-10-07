package api

import (
	"context"
	"errors"
	commonapi "github.com/addp/common/api"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/client"
	auth "github.com/addp/common/middleware/auth"
	i18n "github.com/addp/common/middleware/i18n"
	monitori18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type ResourceObservationHandler struct {
	service *service.ResourceObservationService
}

func NewResourceObservationHandler(s *service.ResourceObservationService) *ResourceObservationHandler {
	return &ResourceObservationHandler{service: s}
}
func resourceError(c *gin.Context, err error) {
	status, code, key := 503, "observability_backend_unavailable", monitori18n.MsgMetricsUnavailable
	switch {
	case errors.Is(err, metricsdiscovery.ErrDisabled):
		status, code, key = 409, "observability_capability_disabled", monitori18n.MsgMetricsDisabled
	case errors.Is(err, metricsdiscovery.ErrUnconfigured):
		status, code, key = 503, "observability_capability_unconfigured", monitori18n.MsgMetricsUnconfigured
	case errors.Is(err, resourcequery.ErrInvalid):
		status, code, key = 400, "invalid_resource_query", monitori18n.MsgResourceQueryInvalid
	case errors.Is(err, resourcequery.ErrBudget):
		status, code, key = 422, "observability_query_budget_exceeded", monitori18n.MsgResourceQueryBudget
	case errors.Is(err, resourcequery.ErrBusy):
		status, code, key = 429, "observability_query_concurrency_exceeded", monitori18n.MsgResourceQueryBusy
	case errors.Is(err, resourcequery.ErrConflict):
		status, code, key = 409, "resource_version_conflict", monitori18n.MsgConfigurationConflict
	case errors.Is(err, context.DeadlineExceeded):
		status, code, key = 504, "observability_query_timeout", monitori18n.MsgMetricsTimeout
	default:
		if status, ok := client.SystemAPIStatusCode(err); ok && (status == 401 || status == 403 || status == 404) {
			targetError(c, err)
			return
		}
	}
	c.JSON(status, gin.H{"error": i18n.T(c, key), "error_code": code})
}
func resourceQuery(c *gin.Context, trend bool) (string, []string, time.Time, time.Time, resourcequery.Dimensions, bool) {
	if c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 || len(c.Request.URL.RawQuery) > 4096 {
		resourceError(c, resourcequery.ErrInvalid)
		return "", nil, time.Time{}, time.Time{}, nil, false
	}
	q, e := url.ParseQuery(c.Request.URL.RawQuery)
	invalid := e != nil
	for key, values := range q {
		if len(values) != 1 || values[0] == "" || (key != "node_id" && key != "metrics" && key != "device" && key != "mountpoint" && key != "fstype" && (!trend || (key != "start" && key != "end"))) {
			invalid = true
		}
	}
	if q.Get("node_id") == "" || q.Get("metrics") == "" || len(c.Request.URL.RawQuery) > 4096 {
		invalid = true
	}
	dimensions := resourcequery.Dimensions{}
	for _, key := range []string{"device", "mountpoint", "fstype"} {
		if q.Has(key) {
			dimensions[key] = q.Get(key)
		}
	}
	if dimensions.Validate() != nil {
		invalid = true
	}
	var start, end time.Time
	if trend {
		start, e = time.Parse(time.RFC3339, q.Get("start"))
		if e != nil {
			invalid = true
		}
		end, e = time.Parse(time.RFC3339, q.Get("end"))
		if e != nil {
			invalid = true
		}
	}
	if invalid {
		resourceError(c, resourcequery.ErrInvalid)
		return "", nil, start, end, nil, false
	}
	return q.Get("node_id"), strings.Split(q.Get("metrics"), ","), start, end, dimensions, true
}

// Instant godoc
// @Summary 读取节点即时资源 | Read current node resources
// @Description 固定九项标量、五项字节容量及四项 inode；文件系统按挂载维度，字节使用率为 used/(used+available)，inode 使用率为 used/total；inode 总量为零或缺少有效证据返回 no_data | Nine scalar, five byte-capacity and four inode metrics; fixed mount dimensions, byte usage used/(used+available), inode usage used/total; zero inode total or missing evidence returns no_data
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param node_id query string true "节点 UUID | Node UUID"
// @Param metrics query string true "逗号分隔的固定目录键，最多十二项 | Comma separated fixed catalog keys, at most twelve"
// @Param device query string false "文件系统设备精确值，与挂载点和类型一起提供 | Exact filesystem device; requires mountpoint and fstype"
// @Param mountpoint query string false "文件系统挂载点精确值 | Exact filesystem mountpoint"
// @Param fstype query string false "文件系统类型精确值 | Exact filesystem type"
// @Success 200 {object} service.ResourceObservationResponse
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
// @Router /platform/resource_observations [get]
func (h *ResourceObservationHandler) Instant(c *gin.Context) { h.query(c, false) }

// Trend godoc
// @Summary 读取节点资源趋势 | Read node resource trends
// @Description 固定九项标量、五项字节容量及四项 inode；文件系统按挂载维度，字节使用率为 used/(used+available)，inode 使用率为 used/total；inode 总量为零或缺少有效证据返回 no_data | Nine scalar, five byte-capacity and four inode metrics; fixed mount dimensions, byte usage used/(used+available), inode usage used/total; zero inode total or missing evidence returns no_data
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param node_id query string true "节点 UUID | Node UUID"
// @Param metrics query string true "逗号分隔的固定目录键，最多十二项 | Comma separated fixed catalog keys, at most twelve"
// @Param device query string false "文件系统设备精确值，与挂载点和类型一起提供 | Exact filesystem device; requires mountpoint and fstype"
// @Param mountpoint query string false "文件系统挂载点精确值 | Exact filesystem mountpoint"
// @Param fstype query string false "文件系统类型精确值 | Exact filesystem type"
// @Param start query string true "整秒 RFC3339 开始时间 | Whole-second RFC3339 start"
// @Param end query string true "整秒 RFC3339 结束时间 | Whole-second RFC3339 end"
// @Success 200 {object} service.ResourceObservationResponse
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
// @Router /platform/resource_trends [get]
func (h *ResourceObservationHandler) Trend(c *gin.Context) { h.query(c, true) }

func (h *ResourceObservationHandler) query(c *gin.Context, trend bool) {
	node, keys, start, end, dimensions, ok := resourceQuery(c, trend)
	if !ok {
		return
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	value, err := h.service.Query(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), node, targetToken(c), keys, start, end, trend, dimensions)
	if err != nil {
		resourceError(c, err)
		return
	}
	c.Set("resource_query_audit_node", value.Subject.NodeID)
	c.JSON(200, value)
}

// Policy godoc
// @Summary 获取资源查询预算 | Get resource query budget
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.ResourceQueryPolicyResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.configuration.read"]
// @Router /settings/resource-query-policy [get]
func (h *ResourceObservationHandler) Policy(c *gin.Context) {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	value, err := h.service.Policy(c.Request.Context())
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(200, value)
}

// UpdatePolicy godoc
// @Summary 原子更新并即时生效资源查询预算 | Atomically update and apply resource query budget
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body service.ResourceQueryPolicyInput true "版本和完整预算 | Version and complete budget"
// @Success 200 {object} service.ResourceQueryPolicyResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.configuration.update"]
// @Router /settings/resource-query-policy [put]
func (h *ResourceObservationHandler) UpdatePolicy(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	var input service.ResourceQueryPolicyInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if !resourcePolicyBind(c, &input) {
		return
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	value, err := h.service.UpdatePolicy(c.Request.Context(), input, uint(principal))
	if err != nil {
		resourceError(c, err)
		return
	}
	c.Set("resource_query_policy_audit_version", value.Version)
	c.JSON(200, value)
}
func resourcePolicyBind(c *gin.Context, input any) bool {
	if err := commonapi.BindOptionalJSONStrict(c, input); err != nil {
		resourceError(c, resourcequery.ErrInvalid)
		return false
	}
	return true
}

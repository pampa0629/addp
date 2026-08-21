package api

import (
	"errors"
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	moni18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

// StatisticsHandler 统计 Handler
type StatisticsHandler struct {
	statisticsService *service.StatisticsService
}

// NewStatisticsHandler 创建 Handler
func NewStatisticsHandler(statisticsService *service.StatisticsService) *StatisticsHandler {
	return &StatisticsHandler{
		statisticsService: statisticsService,
	}
}

// GetStatistics 获取统计数据
// @Summary 获取统计数据 | Get statistics
// @Tags Monitor
// @Produce json
// @Param module query string false "模块名 | Module"
// @Param duration query string false "统计时长 | Duration" default(24h)
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.statistics.read"]
// @Router /executions/stats [get]
// @Security BearerAuth
func (h *StatisticsHandler) GetStatistics(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	req := &service.StatisticsRequest{
		TenantID: tenantID,
		Module:   c.Query("module"),
		Duration: c.DefaultQuery("duration", "24h"),
	}

	// 获取统计数据
	stats, err := h.statisticsService.GetStatistics(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetTrendData 获取趋势数据
// @Summary 获取趋势数据 | Get trend data
// @Tags Monitor
// @Produce json
// @Param module query string false "模块名 | Module"
// @Param days query int false "天数 | Days" default(7)
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.statistics.read"]
// @Router /executions/trend [get]
// @Security BearerAuth
func (h *StatisticsHandler) GetTrendData(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	// 解析参数
	module := c.Query("module")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))

	// 获取趋势数据
	trendData, err := h.statisticsService.GetTrendData(c.Request.Context(), tenantID, module, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, trendData)
}

// GetExecutionRuntimeMetrics 获取分组执行运行时指标
// @Summary 获取执行运行时指标 | Get execution runtime metrics
// @Description 按模块、任务类型和执行边界聚合窗口内吞吐、失败、重试、恢复与耗时指标；积压数量为查询时的全量当前状态 | Aggregate windowed throughput, failure, retry, recovery, and duration metrics by module, task type, and execution boundary; backlog counts reflect the current state across all time
// @Tags Monitor
// @Produce json
// @Param module query string false "模块名 | Module"
// @Param duration query string false "统计窗口 | Observation window" default(24h) Enums(24h,7d,30d)
// @Success 200 {object} service.ExecutionRuntimeMetricsResponse "执行运行时指标 | Execution runtime metrics"
// @Failure 400 {object} ErrorResponse "统计窗口无效 | Invalid observation window"
// @Failure 500 {object} ErrorResponse "内部错误 | Internal error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.statistics.read"]
// @Router /executions/runtime-metrics [get]
// @Security BearerAuth
func (h *StatisticsHandler) GetExecutionRuntimeMetrics(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	result, err := h.statisticsService.GetExecutionRuntimeMetrics(c.Request.Context(), service.ExecutionRuntimeMetricsRequest{
		TenantID: tenantID,
		Module:   c.Query("module"),
		Duration: c.DefaultQuery("duration", "24h"),
	})
	if errors.Is(err, service.ErrInvalidRuntimeMetricDuration) {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidRuntimeMetricDuration)})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

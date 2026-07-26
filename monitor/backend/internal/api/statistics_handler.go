package api

import (
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
	// 从 context 获取 tenant_id
	tenantIDRaw, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, moni18n.MsgTenantNotFound)})
		return
	}

	// 转换类型：中间件设置的是 uint，需要转换为 int
	tenantID := int(tenantIDRaw.(uint))

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
	// 从 context 获取 tenant_id
	tenantIDRaw, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, moni18n.MsgTenantNotFound)})
		return
	}

	// 转换类型：中间件设置的是 uint，需要转换为 int
	tenantID := int(tenantIDRaw.(uint))

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

package api

import (
	"net/http"
	"strconv"

	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

// Overview godoc
// @Summary 读取平台资源总览与主机 TOP5 | Read platform resource overview and host TOP5
// @Description System 独立核验 platform.host_node.read；完整当前授权纳管主机集合，包含停用节点；固定 CPU 一分钟忙碌率及内存使用率，仅有效样本排序。统一评估时刻，明确有效/缺失/过期/未接入覆盖；抓取成功不代表业务健康。不接受分页/筛选/指标，完整集合超出双指标预算时整份拒绝，默认最多50台，不截断。| System independently requires platform.host_node.read. Complete current authorized managed-host inventory including disabled nodes. Fixed one-minute CPU busy and memory used percentages, ranking valid samples only at one evaluation time, with valid/missing/stale/not-connected coverage. Scrape success is not business health. No paging, filtering or metric input; reject the whole request beyond the two-metric budget, at most 50 hosts by default, without truncation.
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.ResourceOverviewResponse
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
// @Router /platform/resource_overview [get]
func (h *ResourceObservationHandler) Overview(c *gin.Context) {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	var value service.ResourceOverviewResponse
	value, err := h.service.Overview(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), targetToken(c))
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

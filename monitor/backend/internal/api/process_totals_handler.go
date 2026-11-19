package api

import (
	"net/http"
	"strconv"

	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

// ProcessTotals godoc
// @Summary 读取 ADDP 服务资源总量 | Read total ADDP service resources
// @Description 以当前在线实例为分母，返回全平台与按模块的 CPU 核数、进程 RSS 合计以及分指标采样覆盖率 | Return platform and module CPU cores, process RSS totals and per-metric coverage for current online instances
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.ServiceResourceTotals
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.resource_observation.read","platform.module.read"]
// @Router /platform/process_resource_totals [get]
func (h *ResourceObservationHandler) ProcessTotals(c *gin.Context) {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	var value service.ServiceResourceTotals
	value, err := h.service.ProcessTotals(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), targetToken(c))
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

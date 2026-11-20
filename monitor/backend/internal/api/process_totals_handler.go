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
// @Description 按模块和角色返回一分钟 CPU、进程 RSS、容器工作集与覆盖数。持主机读权时包含可见节点的前端和覆盖未知节点数 | Return one-minute CPU, process RSS, container working set and coverage by module and role. Host readers also see frontends from authorized nodes and the number of nodes with unknown coverage
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
	value, err := h.service.ProcessTotals(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), targetToken(c), auth.HasRolePermission(c, "platform.host_node.read"))
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

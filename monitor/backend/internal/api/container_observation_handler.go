package api

import (
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// Container godoc
// @Summary 读取当前实例容器资源 | Read current instance container resources
// @Description 精确授权一个当前实例；四项固定资源组目录，只有 System 部署证明匹配时提供硬上界利用率。证明缺失不推断容器或额度；进程 CPU/RSS 与资源组整体用量分离，私有身份不进入响应。| Authorizes one current instance. Four fixed cgroup metrics; utilization requires matching System deployment evidence. Missing proof never implies a container or allocation. Process CPU/RSS are distinct from cgroup usage; private identities are excluded.
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param instance_ids query string true "恰好一个当前实例 ID | Exactly one current instance ID"
// @Success 200 {object} service.ContainerResourceResponse
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
// @Router /platform/container_resource_observations [get]
func (h *ResourceObservationHandler) Container(c *gin.Context) {
	ids, ok := processSummaryIDs(c, true)
	if !ok {
		return
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	var value service.ContainerResourceResponse
	var err error
	value, err = h.service.ContainerObservation(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), targetToken(c), ids[0])
	if err != nil {
		resourceError(c, err)
		return
	}
	c.Set("resource_query_audit_instances", ids)
	c.JSON(http.StatusOK, value)
}

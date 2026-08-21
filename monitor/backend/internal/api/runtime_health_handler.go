package api

import (
	"net/http"

	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type RuntimeHealthHandler struct {
	service *service.RuntimeHealthService
}

func NewRuntimeHealthHandler(runtimeHealthService *service.RuntimeHealthService) *RuntimeHealthHandler {
	return &RuntimeHealthHandler{service: runtimeHealthService}
}

// ListHealth 查询后台运行实例健康状态
// @Summary 查询后台运行实例健康状态 | List background runtime health
// @Description 分角色返回 execution worker、continuous worker 和 dispatcher 的公共进程心跳；该接口不返回任何 lease token 或 fencing token。| Return public process heartbeats for execution workers, continuous workers, and dispatchers by role; no lease token or fencing token is returned.
// @Tags Monitor
// @Produce json
// @Success 200 {array} service.RuntimeHealthSummary
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.health.read"]
// @Router /runtime-instances/health [get]
// @Security BearerAuth
func (h *RuntimeHealthHandler) ListHealth(c *gin.Context) {
	items, err := h.service.ListHealth(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

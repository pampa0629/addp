package api

import (
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

// InspectAttributes godoc
// @Summary 动态识别标准 attributes | Inspect standard attributes without persistence
// @Description 仅 Transfer Tenant Service 可在核验当前执行及私有来源后识别 Infra 文件 attributes，不写入 Meta item 或 node。| Only the Transfer Tenant Service may inspect Infra file attributes after verifying the current execution and private source; no Meta item or node is persisted.
// @Tags Meta
// @Accept json
// @Produce json
// @Param request body service.InspectRequest true "识别请求 | Inspect request"
// @Success 200 {object} service.InspectResult "识别结果 | Inspect result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "机器身份或权限不满足要求 | Service identity or permission denied"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.inspect.execute"]
// @Router /inspect [post]
// @Security BearerAuth
func (h *Handler) InspectAttributes(c *gin.Context) {
	if h.inspectService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "inspect service not available"})
		return
	}

	var req service.InspectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.inspectService.Inspect(c.Request.Context(), commonAuth.GetTenantID(c), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

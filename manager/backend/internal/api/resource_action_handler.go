package api

import (
	"net/http"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type ResourceActionHandler struct {
	service *service.ResourceActionService
}

func NewResourceActionHandler(service *service.ResourceActionService) *ResourceActionHandler {
	return &ResourceActionHandler{service: service}
}

// GetResourceActions 查询资源可用用户动作。
// @Summary 查询资源动作能力 | Get resource action capabilities
// @Description 按 ResourceLocator 返回当前资源可显示的 Manager 用户动作及格式限制 | Return Manager user actions and format constraints for a resource locator
// @Tags Manager
// @Produce json
// @Param locator query string true "资源定位符 URI | Resource locator URI"
// @Success 200 {object} service.ResourceActionsResponse "资源动作能力 | Resource action capabilities"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read"]
// @Router /resource-actions [get]
// @Security BearerAuth
func (h *ResourceActionHandler) GetResourceActions(c *gin.Context) {
	locator := strings.TrimSpace(c.Query("locator"))
	if locator == "" {
		missingLocator(c)
		return
	}
	resp, err := h.service.GetResourceActions(c.Request.Context(), locator, tenantIDFromContext(c))
	if err != nil {
		if err == service.ErrEngineAccessDenied {
			accessDeniedToEngine(c)
			return
		}
		if strings.Contains(err.Error(), "locator") || strings.Contains(err.Error(), "URI") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, resp)
}

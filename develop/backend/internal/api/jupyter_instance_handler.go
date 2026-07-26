package api

import (
	"net/http"

	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// JupyterInstanceHandler Jupyter 实例管理 API 处理器
type JupyterInstanceHandler struct {
	instanceService *service.JupyterInstanceService
}

// NewJupyterInstanceHandler 创建 Jupyter 实例处理器
func NewJupyterInstanceHandler(instanceService *service.JupyterInstanceService) *JupyterInstanceHandler {
	return &JupyterInstanceHandler{
		instanceService: instanceService,
	}
}

// StartInstance 启动 Jupyter 实例
// @Summary 启动 Jupyter 实例 | Start Jupyter instance
// @Tags Jupyter Instance
// @Produce json
// @Success 200 {object} service.JupyterInstance "实例信息 | Instance information"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.execute"]
// @Router /jupyter/instance/start [post]
func (h *JupyterInstanceHandler) StartInstance(c *gin.Context) {
	instance, err := h.instanceService.StartInstance(c.Request.Context(), tenantIDValue(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "启动 Jupyter 实例失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, instance)
}

// StopInstance 停止 Jupyter 实例
// @Summary 停止 Jupyter 实例 | Stop Jupyter instance
// @Tags Jupyter Instance
// @Produce json
// @Success 200 {object} map[string]string "停止成功 | Stopped successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.execute"]
// @Router /jupyter/instance/stop [post]
func (h *JupyterInstanceHandler) StopInstance(c *gin.Context) {
	if err := h.instanceService.StopInstance(c.Request.Context(), tenantIDValue(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "停止 Jupyter 实例失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Jupyter 实例已停止",
	})
}

// GetInstanceStatus 获取 Jupyter 实例状态
// @Summary 获取 Jupyter 实例状态 | Get Jupyter instance status
// @Tags Jupyter Instance
// @Produce json
// @Success 200 {object} service.JupyterInstance "实例状态 | Instance status"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.read"]
// @Router /jupyter/instance/status [get]
func (h *JupyterInstanceHandler) GetInstanceStatus(c *gin.Context) {
	instance, err := h.instanceService.GetInstance(c.Request.Context(), tenantIDValue(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "查询 Jupyter 实例失败",
			"details": err.Error(),
		})
		return
	}

	if instance == nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "not_found",
			"message": "Jupyter 实例不存在",
		})
		return
	}

	c.JSON(http.StatusOK, instance)
}

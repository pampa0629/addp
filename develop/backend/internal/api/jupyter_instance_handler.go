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
// @Summary 启动 Jupyter 实例
// @Tags Jupyter Instance
// @Produce json
// @Success 200 {object} service.JupyterInstance
// @Router /api/develop/jupyter/instance/start [post]
func (h *JupyterInstanceHandler) StartInstance(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")

	instance, err := h.instanceService.StartInstance(c.Request.Context(), tenantID.(uint))
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
// @Summary 停止 Jupyter 实例
// @Tags Jupyter Instance
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/develop/jupyter/instance/stop [post]
func (h *JupyterInstanceHandler) StopInstance(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")

	if err := h.instanceService.StopInstance(c.Request.Context(), tenantID.(uint)); err != nil {
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
// @Summary 获取 Jupyter 实例状态
// @Tags Jupyter Instance
// @Produce json
// @Success 200 {object} service.JupyterInstance
// @Router /api/develop/jupyter/instance/status [get]
func (h *JupyterInstanceHandler) GetInstanceStatus(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")

	instance, err := h.instanceService.GetInstance(c.Request.Context(), tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "查询 Jupyter 实例失败",
			"details": err.Error(),
		})
		return
	}

	if instance == nil {
		c.JSON(http.StatusOK, gin.H{
			"status": "not_found",
			"message": "Jupyter 实例不存在",
		})
		return
	}

	c.JSON(http.StatusOK, instance)
}

// ListInstances 列出所有 Jupyter 实例（管理员）
// @Summary 列出所有 Jupyter 实例
// @Tags Jupyter Instance
// @Produce json
// @Success 200 {array} service.JupyterInstance
// @Router /api/develop/jupyter/instances [get]
func (h *JupyterInstanceHandler) ListInstances(c *gin.Context) {
	instances, err := h.instanceService.ListInstances(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "查询 Jupyter 实例列表失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"instances": instances,
		"total":     len(instances),
	})
}

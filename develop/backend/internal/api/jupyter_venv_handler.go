package api

import (
	"net/http"

	"github.com/addp/common/logger"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// JupyterVenvHandler Jupyter 虚拟环境管理 Handler
type JupyterVenvHandler struct {
	venvService *service.JupyterVenvService
}

// NewJupyterVenvHandler 创建 Handler
func NewJupyterVenvHandler(venvService *service.JupyterVenvService) *JupyterVenvHandler {
	return &JupyterVenvHandler{
		venvService: venvService,
	}
}

// GetVenvStatus 获取租户虚拟环境状态
// @Summary 获取 Jupyter 虚拟环境状态 | Get Jupyter venv status
// @Tags Jupyter
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.read"]
// @Router /jupyter/venv/status [get]
// @Security BearerAuth
func (h *JupyterVenvHandler) GetVenvStatus(c *gin.Context) {
	tenantIDUint := tenantIDValue(c)

	// 获取虚拟环境信息
	info, err := h.venvService.GetTenantVenvInfo(c.Request.Context(), tenantIDUint)
	if err != nil {
		logger.L().Error("获取租户虚拟环境信息失败", "tenant_id", tenantIDUint, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取信息失败", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, info)
}

// InitVenv 初始化租户虚拟环境
// @Summary 初始化 Jupyter 虚拟环境 | Init Jupyter venv
// @Tags Jupyter
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.create"]
// @Router /jupyter/venv/init [post]
// @Security BearerAuth
func (h *JupyterVenvHandler) InitVenv(c *gin.Context) {
	tenantIDUint := tenantIDValue(c)

	logger.L().Info("开始初始化租户虚拟环境", "tenant_id", tenantIDUint)

	// 初始化虚拟环境
	info, err := h.venvService.InitTenantVenv(c.Request.Context(), tenantIDUint)
	if err != nil {
		logger.L().Error("初始化租户虚拟环境失败", "tenant_id", tenantIDUint, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "初始化失败", "details": err.Error()})
		return
	}

	logger.L().Info("租户虚拟环境初始化成功", "tenant_id", tenantIDUint, "kernel_name", info.KernelName)

	c.JSON(http.StatusOK, gin.H{
		"message": "虚拟环境初始化成功",
		"data":    info,
	})
}

// DeleteVenv 删除租户虚拟环境
// @Summary 删除 Jupyter 虚拟环境 | Delete Jupyter venv
// @Tags Jupyter
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.delete"]
// @Router /jupyter/venv [delete]
// @Security BearerAuth
func (h *JupyterVenvHandler) DeleteVenv(c *gin.Context) {
	tenantIDUint := tenantIDValue(c)

	logger.L().Warn("删除租户虚拟环境请求", "tenant_id", tenantIDUint)

	// 删除虚拟环境
	err := h.venvService.DeleteTenantVenv(c.Request.Context(), tenantIDUint)
	if err != nil {
		logger.L().Error("删除租户虚拟环境失败", "tenant_id", tenantIDUint, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "虚拟环境已删除"})
}

// GetJupyterServerStatus 获取 Jupyter Server 状态
// @Summary 获取 Jupyter Server 状态 | Get Jupyter server status
// @Tags Jupyter
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.read"]
// @Router /jupyter/server/status [get]
// @Security BearerAuth
func (h *JupyterVenvHandler) GetJupyterServerStatus(c *gin.Context) {
	status := h.venvService.GetJupyterServerStatus(c.Request.Context())
	c.JSON(http.StatusOK, status)
}

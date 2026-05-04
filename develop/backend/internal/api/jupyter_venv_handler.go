package api

import (
	"net/http"
	"strconv"

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
// @Router /jupyter/venv/status [get]
// @Security BearerAuth
func (h *JupyterVenvHandler) GetVenvStatus(c *gin.Context) {
	// 从 JWT token 中获取租户 ID
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权: 缺少租户信息"})
		return
	}

	tenantIDUint, ok := tenantID.(uint)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的租户 ID"})
		return
	}

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
// @Router /jupyter/venv/init [post]
// @Security BearerAuth
func (h *JupyterVenvHandler) InitVenv(c *gin.Context) {
	// 从 JWT token 中获取租户 ID
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权: 缺少租户信息"})
		return
	}

	tenantIDUint, ok := tenantID.(uint)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的租户 ID"})
		return
	}

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
// @Router /jupyter/venv [delete]
// @Security BearerAuth
func (h *JupyterVenvHandler) DeleteVenv(c *gin.Context) {
	// 从 JWT token 中获取租户 ID
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权: 缺少租户信息"})
		return
	}

	tenantIDUint, ok := tenantID.(uint)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的租户 ID"})
		return
	}

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

// ListVenvs 列出所有租户的虚拟环境 (管理员)
// @Summary 列出 Jupyter 虚拟环境 | List Jupyter venvs
// @Tags Jupyter
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /jupyter/venvs [get]
// @Security BearerAuth
func (h *JupyterVenvHandler) ListVenvs(c *gin.Context) {
	// 检查管理员权限
	isSuperAdmin, exists := c.Get("is_super_admin")
	if !exists || !isSuperAdmin.(bool) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足: 仅超级管理员可访问"})
		return
	}

	// 列出所有虚拟环境
	infos, err := h.venvService.ListTenantVenvs(c.Request.Context())
	if err != nil {
		logger.L().Error("列出租户虚拟环境失败", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  infos,
		"total": len(infos),
	})
}

// GetJupyterServerStatus 获取 Jupyter Server 状态
// @Summary 获取 Jupyter Server 状态 | Get Jupyter server status
// @Tags Jupyter
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /jupyter/server/status [get]
// @Security BearerAuth
func (h *JupyterVenvHandler) GetJupyterServerStatus(c *gin.Context) {
	status := h.venvService.GetJupyterServerStatus(c.Request.Context())
	c.JSON(http.StatusOK, status)
}

// InitVenvByID 为指定租户初始化虚拟环境 (管理员)
// @Summary 为指定租户初始化 Jupyter 虚拟环境 | Init Jupyter venv by tenant
// @Tags Jupyter
// @Produce json
// @Param tenant_id path int true "租户ID | Tenant ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /jupyter/venv/{tenant_id}/init [post]
// @Security BearerAuth
func (h *JupyterVenvHandler) InitVenvByID(c *gin.Context) {
	// 检查管理员权限
	isSuperAdmin, exists := c.Get("is_super_admin")
	if !exists || !isSuperAdmin.(bool) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足: 仅超级管理员可访问"})
		return
	}

	// 解析租户 ID
	tenantIDStr := c.Param("tenant_id")
	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的租户 ID"})
		return
	}

	logger.L().Info("管理员初始化租户虚拟环境", "tenant_id", tenantID)

	// 初始化虚拟环境
	info, err := h.venvService.InitTenantVenv(c.Request.Context(), uint(tenantID))
	if err != nil {
		logger.L().Error("初始化租户虚拟环境失败", "tenant_id", tenantID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "初始化失败", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "虚拟环境初始化成功",
		"data":    info,
	})
}

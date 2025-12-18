package api

import (
	"net/http"

	commonClient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

// ResourceHandler 资源管理 API 处理器
type ResourceHandler struct {
	systemClient *commonClient.SystemClient
}

// NewResourceHandler 创建资源处理器
func NewResourceHandler(systemClient *commonClient.SystemClient) *ResourceHandler {
	return &ResourceHandler{
		systemClient: systemClient,
	}
}

// ListResources 获取数据源列表（供 SQL 编辑器使用）
// @Summary 获取可用于 SQL 查询的数据源列表
// @Tags Resources
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/resources [get]
func (h *ResourceHandler) ListResources(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 从 System 模块获取所有支持 SQL 查询的资源
	resources, err := h.systemClient.ListSQLQueryEngines(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取资源列表失败",
			"details": err.Error(),
		})
		return
	}

	// 返回资源列表（与前端期望的格式一致）
	c.JSON(http.StatusOK, resources)
}

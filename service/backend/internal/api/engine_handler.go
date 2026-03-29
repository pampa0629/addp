package api

import (
	"net/http"

	commonClient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

// EngineHandler 引擎管理 API 处理器
type EngineHandler struct {
	systemClient *commonClient.SystemClient
}

// NewEngineHandler 创建引擎处理器
func NewEngineHandler(systemClient *commonClient.SystemClient) *EngineHandler {
	return &EngineHandler{
		systemClient: systemClient,
	}
}

// ListEngines 获取 PostgreSQL 数据源列表（供内部服务创建使用）
// @Summary 获取可用于数据服务的 PostgreSQL 引擎列表
// @Tags Engines
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /engines [get]
func (h *EngineHandler) ListEngines(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 从 System 模块获取所有 PostgreSQL 引擎
	engines, err := h.systemClient.ListEngines("postgresql", tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取引擎列表失败",
			"details": err.Error(),
		})
		return
	}

	// 直接返回引擎列表（与 develop 模块保持一致）
	c.JSON(http.StatusOK, engines)
}

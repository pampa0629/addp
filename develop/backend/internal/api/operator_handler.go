package api

import (
	"net/http"

	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// OperatorHandler 算子发现API处理器
type OperatorHandler struct {
	operatorDiscovery *service.OperatorDiscoveryService
}

// NewOperatorHandler 创建算子处理器
func NewOperatorHandler(operatorDiscovery *service.OperatorDiscoveryService) *OperatorHandler {
	return &OperatorHandler{
		operatorDiscovery: operatorDiscovery,
	}
}

// ListAllOperators 获取所有算子
// @Summary 获取所有模块的算子列表
// @Tags Operator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/operators [get]
func (h *OperatorHandler) ListAllOperators(c *gin.Context) {
	operators, err := h.operatorDiscovery.DiscoverAllOperators(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"operators": operators,
		"count":     len(operators),
	})
}

// ListOperatorsByModule 获取指定模块的算子
// @Summary 获取指定模块的算子列表
// @Tags Operator
// @Produce json
// @Param module path string true "模块名称" Enums(geopandas, meta, transfer, manager)
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/operators/modules/{module} [get]
func (h *OperatorHandler) ListOperatorsByModule(c *gin.Context) {
	module := c.Param("module")

	operators, err := h.operatorDiscovery.GetOperatorsByModule(c.Request.Context(), module)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"module":    module,
		"operators": operators,
		"count":     len(operators),
	})
}

// GetOperatorDetail 获取算子详情
// @Summary 获取算子详情
// @Tags Operator
// @Produce json
// @Param name path string true "算子名称"
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/operators/{name} [get]
func (h *OperatorHandler) GetOperatorDetail(c *gin.Context) {
	name := c.Param("name")

	operator, err := h.operatorDiscovery.GetOperatorDetail(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"operator": operator,
	})
}

// RefreshCache 刷新算子缓存
// @Summary 刷新算子缓存
// @Tags Operator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/operators/refresh [post]
func (h *OperatorHandler) RefreshCache(c *gin.Context) {
	if err := h.operatorDiscovery.RefreshCache(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "缓存已刷新",
	})
}

// GetCacheInfo 获取缓存信息
// @Summary 获取缓存信息
// @Tags Operator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/operators/cache/info [get]
func (h *OperatorHandler) GetCacheInfo(c *gin.Context) {
	info := h.operatorDiscovery.GetCacheInfo()

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"cache":  info,
	})
}

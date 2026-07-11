package api

import (
	"errors"
	"net/http"

	"github.com/addp/common/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// RegistryHandler 能力注册 API 处理器
type RegistryHandler struct {
	registryService *service.RegistryService
	engineService   *service.EngineService
}

// NewRegistryHandler 创建能力注册处理器
func NewRegistryHandler(registryService *service.RegistryService, engineService *service.EngineService) *RegistryHandler {
	return &RegistryHandler{
		registryService: registryService,
		engineService:   engineService,
	}
}

// RegisterCapability 注册能力
// POST /api/v1/internal/registry/capabilities
func (h *RegistryHandler) RegisterCapability(c *gin.Context) {
	var req models.CapabilityRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	engineID, err := h.registryService.RegisterCapability(c.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCapabilityRegistration) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 触发异步连接测试
	if err := h.engineService.AsyncCheckConnection(engineID); err != nil {
		// 连接测试失败不影响注册成功
		// 只记录警告日志
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "capability registered successfully",
		"engine_id": engineID,
	})
}

// ListCapabilities 查询能力列表
// GET /api/v1/internal/registry/capabilities?engine_type=geopython_workflow&is_builtin=true
func (h *RegistryHandler) ListCapabilities(c *gin.Context) {
	filters := make(map[string]interface{})

	// 支持的查询参数
	if isBuiltin := c.Query("is_builtin"); isBuiltin != "" {
		filters["is_builtin"] = isBuiltin == "true"
	}
	if engineType := c.Query("engine_type"); engineType != "" {
		filters["engine_type"] = engineType
	}
	if isActive := c.Query("is_active"); isActive != "" {
		filters["is_active"] = isActive == "true"
	}

	engines, err := h.registryService.ListCapabilities(c.Request.Context(), filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, engines)
}

// ListComputeEngines 查询所有具有计算能力的引擎
// GET /api/v1/internal/registry/compute-engines
func (h *RegistryHandler) ListComputeEngines(c *gin.Context) {
	ctx := c.Request.Context()

	engines, err := h.registryService.ListComputeEngines(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, engines)
}

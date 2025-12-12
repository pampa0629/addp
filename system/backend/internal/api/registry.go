package api

import (
	"net/http"

	"github.com/addp/common/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// RegistryHandler 能力注册 API 处理器
type RegistryHandler struct {
	registryService *service.RegistryService
}

// NewRegistryHandler 创建能力注册处理器
func NewRegistryHandler(registryService *service.RegistryService) *RegistryHandler {
	return &RegistryHandler{
		registryService: registryService,
	}
}

// RegisterCapability 注册能力
// POST /internal/registry/capabilities
func (h *RegistryHandler) RegisterCapability(c *gin.Context) {
	var req models.CapabilityRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.registryService.RegisterCapability(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "capability registered successfully"})
}

// ListCapabilities 查询能力列表
// GET /internal/registry/capabilities?resource_type=compute_engine&is_builtin=true
func (h *RegistryHandler) ListCapabilities(c *gin.Context) {
	filters := make(map[string]interface{})

	// 支持的查询参数
	if resourceType := c.Query("resource_type"); resourceType != "" {
		filters["resource_type"] = resourceType
	}
	if isBuiltin := c.Query("is_builtin"); isBuiltin != "" {
		filters["is_builtin"] = isBuiltin == "true"
	}
	if isActive := c.Query("is_active"); isActive != "" {
		filters["is_active"] = isActive == "true"
	}

	resources, err := h.registryService.ListCapabilities(c.Request.Context(), filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resources)
}

// GetCapabilityByIdentifier 根据 unique_identifier 查询能力
// GET /internal/registry/capabilities/:identifier
func (h *RegistryHandler) GetCapabilityByIdentifier(c *gin.Context) {
	identifier := c.Param("identifier")
	if identifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifier is required"})
		return
	}

	resource, err := h.registryService.GetCapabilityByIdentifier(c.Request.Context(), identifier)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resource)
}

// ListComputeResources 查询所有具有计算能力的资源
// GET /internal/registry/compute-resources
func (h *RegistryHandler) ListComputeResources(c *gin.Context) {
	ctx := c.Request.Context()

	resources, err := h.registryService.ListComputeResources(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resources)
}

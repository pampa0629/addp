package api

import (
	"net/http"
	"strconv"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// TileServiceHandler 瓦片服务 HTTP 处理器
// ============================================================================

type TileServiceHandler struct {
	tileServiceService *service.TileServiceService
}

func NewTileServiceHandler(tileServiceService *service.TileServiceService) *TileServiceHandler {
	return &TileServiceHandler{
		tileServiceService: tileServiceService,
	}
}

// ============================================================================
// 瓦片服务 CRUD
// ============================================================================

// CreateTileService 创建瓦片服务
// @Summary 创建瓦片服务
// @Tags TileService
// @Accept json
// @Produce json
// @Param body body models.CreateTileServiceRequest true "创建瓦片服务请求"
// @Success 201 {object} models.TileServiceDTO
// @Router /api/service/tile [post]
func (h *TileServiceHandler) CreateTileService(c *gin.Context) {
	// 1. 解析请求体
	var req models.CreateTileServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 2. 获取租户 ID 和用户 ID（从 JWT 中间件）
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Tenant ID not found"})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found"})
		return
	}

	// 3. 调用服务层创建瓦片服务
	tileServiceDTO, err := h.tileServiceService.CreateService(
		&req,
		tenantID.(uint),
		userID.(uint),
	)
	if err != nil {
		// 根据错误类型返回不同状态码
		if err.Error() == "service name already exists" {
			c.JSON(http.StatusConflict, gin.H{"error": "服务名称已存在，请使用其他名称"})
			return
		}
		if err.Error() == "first_layer is required" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "必须提供第一个图层配置"})
			return
		}
		// 其他错误返回 500
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回创建结果
	c.JSON(http.StatusCreated, tileServiceDTO)
}

// GetTileService 获取瓦片服务详情
// @Summary 获取瓦片服务详情
// @Tags TileService
// @Produce json
// @Param id path int true "服务 ID"
// @Success 200 {object} models.TileServiceDTO
// @Router /api/service/tile/{id} [get]
func (h *TileServiceHandler) GetTileService(c *gin.Context) {
	// 1. 解析路径参数
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// 2. 调用服务层获取瓦片服务
	tileServiceDTO, err := h.tileServiceService.GetService(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// 3. 返回服务详情
	c.JSON(http.StatusOK, tileServiceDTO)
}

// GetTileServiceByName 根据名称获取瓦片服务
// @Summary 根据名称获取瓦片服务
// @Tags TileService
// @Produce json
// @Param serviceName path string true "服务名称"
// @Success 200 {object} models.TileServiceDTO
// @Router /api/service/tile/by-name/{serviceName} [get]
func (h *TileServiceHandler) GetTileServiceByName(c *gin.Context) {
	// 1. 解析路径参数
	serviceName := c.Param("serviceName")

	// 2. 获取租户 ID（从 JWT 中间件）
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Tenant ID not found"})
		return
	}

	// 3. 调用服务层获取瓦片服务
	tileServiceDTO, err := h.tileServiceService.GetServiceByNameAndTenant(serviceName, tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// 4. 返回服务详情
	c.JSON(http.StatusOK, tileServiceDTO)
}

// ListTileServices 列出租户下的所有瓦片服务
// @Summary 列出瓦片服务
// @Tags TileService
// @Produce json
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} map[string]interface{}
// @Router /api/service/tile [get]
func (h *TileServiceHandler) ListTileServices(c *gin.Context) {
	// 1. 解析查询参数
	offsetStr := c.DefaultQuery("offset", "0")
	limitStr := c.DefaultQuery("limit", "20")

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 20
	}

	// 2. 获取租户 ID（从 JWT 中间件）
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Tenant ID not found"})
		return
	}

	// 3. 调用服务层列出瓦片服务
	services, total, err := h.tileServiceService.ListServices(tenantID.(uint), offset, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回列表结果
	c.JSON(http.StatusOK, gin.H{
		"data":   services,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	})
}

// SearchTileServices 搜索瓦片服务
// @Summary 搜索瓦片服务
// @Tags TileService
// @Produce json
// @Param keyword query string true "搜索关键词"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} map[string]interface{}
// @Router /api/service/tile/search [get]
func (h *TileServiceHandler) SearchTileServices(c *gin.Context) {
	// 1. 解析查询参数
	keyword := c.Query("keyword")
	if keyword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyword is required"})
		return
	}

	offsetStr := c.DefaultQuery("offset", "0")
	limitStr := c.DefaultQuery("limit", "20")

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 20
	}

	// 2. 获取租户 ID（从 JWT 中间件）
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Tenant ID not found"})
		return
	}

	// 3. 调用服务层搜索瓦片服务
	services, total, err := h.tileServiceService.SearchServices(tenantID.(uint), keyword, offset, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回搜索结果
	c.JSON(http.StatusOK, gin.H{
		"data":    services,
		"total":   total,
		"offset":  offset,
		"limit":   limit,
		"keyword": keyword,
	})
}

// UpdateTileService 更新瓦片服务
// @Summary 更新瓦片服务
// @Tags TileService
// @Accept json
// @Produce json
// @Param id path int true "服务 ID"
// @Param body body models.UpdateTileServiceRequest true "更新瓦片服务请求"
// @Success 200 {object} models.TileServiceDTO
// @Router /api/service/tile/{id} [put]
func (h *TileServiceHandler) UpdateTileService(c *gin.Context) {
	// 1. 解析路径参数
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// 2. 解析请求体
	var req models.UpdateTileServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. 调用服务层更新瓦片服务
	tileServiceDTO, err := h.tileServiceService.UpdateService(uint(id), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回更新结果
	c.JSON(http.StatusOK, tileServiceDTO)
}

// DeleteTileService 删除瓦片服务
// @Summary 删除瓦片服务
// @Tags TileService
// @Produce json
// @Param id path int true "服务 ID"
// @Success 204
// @Router /api/service/tile/{id} [delete]
func (h *TileServiceHandler) DeleteTileService(c *gin.Context) {
	// 1. 解析路径参数
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// 2. 调用服务层删除瓦片服务
	if err := h.tileServiceService.DeleteService(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 3. 返回成功
	c.Status(http.StatusNoContent)
}

// ============================================================================
// 图层管理
// ============================================================================

// AddLayer 为服务添加新图层
// @Summary 添加图层
// @Tags TileService
// @Accept json
// @Produce json
// @Param serviceId path int true "服务 ID"
// @Param body body models.CreateTileLayerRequest true "创建图层请求"
// @Success 201 {object} models.TileServiceLayerDTO
// @Router /api/service/tile/{serviceId}/layers [post]
func (h *TileServiceHandler) AddLayer(c *gin.Context) {
	// 1. 解析路径参数
	serviceIDStr := c.Param("serviceId")
	serviceID, err := strconv.ParseUint(serviceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// 2. 解析请求体
	var req models.CreateTileLayerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. 调用服务层添加图层
	layerDTO, err := h.tileServiceService.AddLayer(uint(serviceID), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回创建结果
	c.JSON(http.StatusCreated, layerDTO)
}

// GetLayer 获取图层详情
// @Summary 获取图层详情
// @Tags TileService
// @Produce json
// @Param serviceId path int true "服务 ID"
// @Param layerId path int true "图层 ID"
// @Success 200 {object} models.TileServiceLayerDTO
// @Router /api/service/tile/{serviceId}/layers/{layerId} [get]
func (h *TileServiceHandler) GetLayer(c *gin.Context) {
	// 1. 解析路径参数
	layerIDStr := c.Param("layerId")
	layerID, err := strconv.ParseUint(layerIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid layer ID"})
		return
	}

	// 2. 调用服务层获取图层
	layerDTO, err := h.tileServiceService.GetLayer(uint(layerID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Layer not found"})
		return
	}

	// 3. 返回图层详情
	c.JSON(http.StatusOK, layerDTO)
}

// ListLayers 列出服务下的所有图层
// @Summary 列出图层
// @Tags TileService
// @Produce json
// @Param serviceId path int true "服务 ID"
// @Success 200 {object} []models.TileServiceLayerDTO
// @Router /api/service/tile/{serviceId}/layers [get]
func (h *TileServiceHandler) ListLayers(c *gin.Context) {
	// 1. 解析路径参数
	serviceIDStr := c.Param("serviceId")
	serviceID, err := strconv.ParseUint(serviceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// 2. 调用服务层列出图层
	layers, err := h.tileServiceService.ListLayers(uint(serviceID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 3. 返回图层列表
	c.JSON(http.StatusOK, layers)
}

// UpdateLayer 更新图层
// @Summary 更新图层
// @Tags TileService
// @Accept json
// @Produce json
// @Param serviceId path int true "服务 ID"
// @Param layerId path int true "图层 ID"
// @Param body body models.UpdateTileLayerRequest true "更新图层请求"
// @Success 200 {object} models.TileServiceLayerDTO
// @Router /api/service/tile/{serviceId}/layers/{layerId} [put]
func (h *TileServiceHandler) UpdateLayer(c *gin.Context) {
	// 1. 解析路径参数
	layerIDStr := c.Param("layerId")
	layerID, err := strconv.ParseUint(layerIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid layer ID"})
		return
	}

	// 2. 解析请求体
	var req models.UpdateTileLayerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. 调用服务层更新图层
	layerDTO, err := h.tileServiceService.UpdateLayer(uint(layerID), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回更新结果
	c.JSON(http.StatusOK, layerDTO)
}

// DeleteLayer 删除图层
// @Summary 删除图层
// @Tags TileService
// @Produce json
// @Param serviceId path int true "服务 ID"
// @Param layerId path int true "图层 ID"
// @Success 204
// @Router /api/service/tile/{serviceId}/layers/{layerId} [delete]
func (h *TileServiceHandler) DeleteLayer(c *gin.Context) {
	// 1. 解析路径参数
	layerIDStr := c.Param("layerId")
	layerID, err := strconv.ParseUint(layerIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid layer ID"})
		return
	}

	// 2. 调用服务层删除图层
	if err := h.tileServiceService.DeleteLayer(uint(layerID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 3. 返回成功
	c.Status(http.StatusNoContent)
}

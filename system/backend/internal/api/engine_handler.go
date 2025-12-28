package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonutils "github.com/addp/common/utils"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type EngineHandler struct {
	engineService        *service.EngineService
	storageEngineService *service.StorageEngineService
}

func NewEngineHandler(engineService *service.EngineService) *EngineHandler {
	return &EngineHandler{
		engineService:        engineService,
		storageEngineService: service.NewStorageEngineService(),
	}
}

func (h *EngineHandler) Create(c *gin.Context) {
	var req models.EngineCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetUint("user_id")
	engine, err := h.engineService.Create(&req, userID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, engine)
}

func (h *EngineHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	engineType := c.Query("engine_type")

	// 获取当前用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	engines, err := h.engineService.List(page, pageSize, engineType, userID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, engines)
}

func (h *EngineHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	engine, err := h.engineService.GetByID(uint(id), userID.(uint))
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	c.JSON(http.StatusOK, engine)
}

func (h *EngineHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	var req models.EngineUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	currentUserID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	engine, err := h.engineService.Update(uint(id), &req, currentUserID.(uint))
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	c.JSON(http.StatusOK, engine)
}

func (h *EngineHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	currentUserID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	if err := h.engineService.Delete(uint(id), currentUserID.(uint)); err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

func (h *EngineHandler) respondWithResourceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrResourceNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrResourceForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrBuiltinResourceImmutable):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

// TestConnection 测试存储引擎连接（用户手动触发，同步返回结果）
// POST /api/engines/:id/test-connection
// 测试完成后同步更新连接状态缓存
func (h *EngineHandler) TestConnection(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	currentUserID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	engine, err := h.engineService.GetForConnection(uint(id), currentUserID.(uint))
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	// 测试连接
	if err := h.storageEngineService.TestConnection(engine); err != nil {
		// 更新为offline
		h.engineService.UpdateConnectionStatus(uint(id), "offline", err.Error())

		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "连接失败",
			"error":   err.Error(),
		})
		return
	}

	// 更新为online
	h.engineService.UpdateConnectionStatus(uint(id), "online", "连接正常")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "连接成功",
	})
}

// TestConnectionBeforeCreate 创建前测试连接
func (h *EngineHandler) TestConnectionBeforeCreate(c *gin.Context) {
	var req models.EngineCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// DEBUG: 打印接收到的连接信息
	if password, ok := req.ConnectionInfo["password"].(string); ok {
		fmt.Printf("[DEBUG] TestConnection received password: '%s', length: %d\n", password, len(password))
	} else {
		fmt.Printf("[DEBUG] TestConnection password field type: %T, value: %v\n", req.ConnectionInfo["password"], req.ConnectionInfo["password"])
	}

	// 构建临时资源对象用于测试
	engine := &models.Engine{
		EngineType:     req.EngineType,
		ConnectionInfo: req.ConnectionInfo,
	}

	// 测试连接
	if err := h.storageEngineService.TestConnection(engine); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "连接失败",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "连接成功",
	})
}

type internalResourceCreateRequest struct {
	models.EngineCreateRequest
	TenantID  *uint `json:"tenant_id"`
	CreatedBy *uint `json:"created_by"`
}

// CreateInternal 内部服务创建资源
func (h *EngineHandler) CreateInternal(c *gin.Context) {
	var req internalResourceCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var tenantID uint
	if req.TenantID != nil {
		tenantID = *req.TenantID
	}

	engine, err := h.engineService.CreateInternal(&req.EngineCreateRequest, tenantID, req.CreatedBy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, engine)
}

// ============ 内部 API（服务间调用）============

// ListInternal 内部资源列表查询（无需用户认证，用于服务间调用）
func (h *EngineHandler) ListInternal(c *gin.Context) {
	engineType := c.Query("engine_type")
	tenantID := c.Query("tenant_id") // 可选，按租户过滤

	// 新增：能力过滤参数
	storageType := c.Query("storage_type") // 可选：如 "relational_db,object_storage"
	computeType := c.Query("compute_type") // 可选：如 "sql_query,scan"

	// 内部调用返回所有资源（或按 tenant_id 过滤）
	var tenantIDUint uint
	if tenantID != "" {
		id, err := strconv.ParseUint(tenantID, 10, 32)
		if err == nil {
			tenantIDUint = uint(id)
		}
	}

	// 如果指定了 capability 过滤条件，使用新的过滤方法
	if storageType != "" || computeType != "" {
		filter := commonutils.CapabilityFilter{
			StorageTypes: parseCommaSeparated(storageType),
			ComputeTypes: parseCommaSeparated(computeType),
		}

		engines, err := h.engineService.ListInternalWithCapability(tenantIDUint, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, engines)
		return
	}

	// 保持原有逻辑（向后兼容）
	engines, err := h.engineService.ListInternal(engineType, tenantIDUint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, engines)
}

// parseCommaSeparated 解析逗号分隔的字符串
func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// GetByIDInternal 内部资源详情查询（无需用户认证，用于服务间调用）
func (h *EngineHandler) GetByIDInternal(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	engine, err := h.engineService.GetByIDInternal(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "资源不存在"})
		return
	}

	c.JSON(http.StatusOK, engine)
}

// ListSchemas 列出指定资源的所有Schema/Database
// @Summary 列出Schema列表
// @Tags Resources
// @Produce json
// @Param id path int true "资源ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/engines/:id/schemas [get]
func (h *EngineHandler) ListSchemas(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	currentUserID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	// 获取资源(验证权限)
	engine, err := h.engineService.GetForConnection(uint(id), currentUserID.(uint))
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	// 调用 StorageEngineService 列出 schemas
	schemas, err := h.storageEngineService.ListSchemas(engine)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"schemas": schemas,
	})
}

// ListTables 列出指定资源和Schema下的所有表
// @Summary 列出表列表
// @Tags Resources
// @Produce json
// @Param id path int true "资源ID"
// @Param schema query string false "Schema名称(默认public)"
// @Success 200 {object} map[string]interface{}
// @Router /api/engines/:id/tables [get]
func (h *EngineHandler) ListTables(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	schema := c.DefaultQuery("schema", "public")

	currentUserID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	// 获取资源(验证权限)
	engine, err := h.engineService.GetForConnection(uint(id), currentUserID.(uint))
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	// 调用 StorageEngineService 列出表
	tables, err := h.storageEngineService.ListTables(engine, schema)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"tables": tables,
	})
}

// TriggerConnectionCheckInternal 触发连接检测（内部API，异步）
// POST /api/internal/engines/:id/check-connection
// 用于其他模块在连接失败时通知System刷新状态
// 立即返回202 Accepted，实际检测在后台执行
func (h *EngineHandler) TriggerConnectionCheckInternal(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	// 异步检测，立即返回
	if err := h.engineService.AsyncCheckConnection(uint(id)); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "连接检测已启动，稍后刷新获取最新状态",
	})
}

// UpdateConnectionStatusRequest 更新连接状态请求
type UpdateConnectionStatusRequest struct {
	ConnectionStatus string `json:"connection_status" binding:"required,oneof=online offline unknown checking"`
	CheckMessage     string `json:"check_message"`
}

// UpdateConnectionStatusInternal 内部API：更新资源连接状态
// PUT /api/internal/engines/:id/connection-status
// 用于Meta模块在后台检测后更新资源连接状态缓存
// 注意：此方法已废弃，建议使用TriggerConnectionCheckInternal让System自己检测
// 保留是为了向后兼容
func (h *EngineHandler) UpdateConnectionStatusInternal(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	var req UpdateConnectionStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 更新连接状态
	if err := h.engineService.UpdateConnectionStatus(uint(id), req.ConnectionStatus, req.CheckMessage); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "连接状态已更新",
	})
}

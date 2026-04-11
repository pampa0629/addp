package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
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

// Create godoc
// @Summary      创建引擎 | Create engine
// @Description  创建新的存储引擎连接 | Create a new storage engine connection
// @Tags         引擎管理 | Engine Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.EngineCreateRequest true "引擎信息 | Engine info"
// @Success      201 {object} models.Engine
// @Failure      400 {object} models.ErrorResponse
// @Router       /engines [post]
func (h *EngineHandler) Create(c *gin.Context) {
	var req models.EngineCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	engine, err := h.engineService.Create(&req, userID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	commonapi.RespondCreated(c, engine)
}

// List godoc
// @Summary      获取引擎列表 | List engines
// @Description  分页获取引擎列表（支持按类型过滤）| Get paginated engine list with type filtering
// @Tags         引擎管理 | Engine Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number" default(1)
// @Param        page_size query int false "每页数量 | Page size" default(10)
// @Param        engine_type query string false "引擎类型 | Engine type"
// @Success      200 {object} object{data=[]models.Engine,total=int,page=int,page_size=int}
// @Failure      500 {object} models.ErrorResponse
// @Router       /engines [get]
func (h *EngineHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	engineType := c.Query("engine_type")

	userID, _ := commonapi.GetCurrentUserID(c)
	engines, total, err := h.engineService.List(page, pageSize, engineType, userID)
	if err != nil {
		commonapi.RespondError(c, 500, err.Error())
		return
	}
	commonapi.RespondPaginated(c, engines, total, page, pageSize)
}

func (h *EngineHandler) GetByID(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	engine, err := h.engineService.GetByID(id, userID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	commonapi.RespondSuccess(c, engine)
}

func (h *EngineHandler) Update(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req models.EngineUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	engine, err := h.engineService.Update(id, &req, userID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	commonapi.RespondSuccess(c, engine)
}

func (h *EngineHandler) Delete(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	if err := h.engineService.Delete(id, userID); err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	commonapi.RespondSuccess(c, gin.H{"message": "删除成功"})
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
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	engine, err := h.engineService.GetForConnection(id, userID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	// 测试连接
	if err := h.storageEngineService.TestConnection(engine); err != nil {
		// 更新为offline
		h.engineService.UpdateConnectionStatus(id, "offline", err.Error())

		commonapi.RespondSuccess(c, gin.H{
			"success": false,
			"message": "连接失败",
			"error":   err.Error(),
		})
		return
	}

	// 更新为online
	h.engineService.UpdateConnectionStatus(id, "online", "连接正常")

	commonapi.RespondSuccess(c, gin.H{
		"success": true,
		"message": "连接成功",
	})
}

// TestConnectionBeforeCreate 创建前测试连接
func (h *EngineHandler) TestConnectionBeforeCreate(c *gin.Context) {
	var req models.EngineCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 构建临时资源对象用于测试
	engine := &models.Engine{
		EngineType:     req.EngineType,
		ConnectionInfo: req.ConnectionInfo,
	}

	// 测试连接
	if err := h.storageEngineService.TestConnection(engine); err != nil {
		commonapi.RespondSuccess(c, gin.H{
			"success": false,
			"message": "连接失败",
			"error":   err.Error(),
		})
		return
	}

	commonapi.RespondSuccess(c, gin.H{
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

	// 内部调用返回所有资源（或按 tenant_id 过滤）
	var tenantIDUint uint
	if tenantID != "" {
		id, err := strconv.ParseUint(tenantID, 10, 32)
		if err == nil {
			tenantIDUint = uint(id)
		}
	}

	// 如果指定了 capability 过滤条件，使用新的过滤方法
	if storageType != "" {
		filter := commonutils.CapabilityFilter{
			StorageTypes: parseCommaSeparated(storageType),
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
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	engine, err := h.engineService.GetByIDInternal(id)
	if err != nil {
		commonapi.RespondError(c, http.StatusNotFound, "资源不存在")
		return
	}

	commonapi.RespondSuccess(c, engine)
}

// ListSchemas 列出指定资源的所有Schema/Database
// @Summary 列出 Schema 列表 | List schemas
// @Tags Resources
// @Produce json
// @Param id path int true "资源ID | Engine ID"
// @Success 200 {object} map[string]interface{}
// @Router /engines/:id/schemas [get]
func (h *EngineHandler) ListSchemas(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)

	// 获取资源(验证权限)
	engine, err := h.engineService.GetForConnection(id, userID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	// 调用 StorageEngineService 列出 schemas
	schemas, err := h.storageEngineService.ListSchemas(engine)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	commonapi.RespondSuccess(c, gin.H{
		"status":  "success",
		"schemas": schemas,
	})
}

// ListTables 列出指定资源和Schema下的所有表
// @Summary 列出表列表 | List tables
// @Tags Resources
// @Produce json
// @Param id path int true "资源ID | Engine ID"
// @Param schema query string false "Schema名称(默认public) | Schema name (default: public)"
// @Success 200 {object} map[string]interface{}
// @Router /engines/:id/tables [get]
func (h *EngineHandler) ListTables(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	schema := c.DefaultQuery("schema", "public")
	userID, _ := commonapi.GetCurrentUserID(c)

	// 获取资源(验证权限)
	engine, err := h.engineService.GetForConnection(id, userID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	// 调用 StorageEngineService 列出表
	tables, err := h.storageEngineService.ListTables(engine, schema)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	commonapi.RespondSuccess(c, gin.H{
		"status": "success",
		"tables": tables,
	})
}

// TriggerConnectionCheckInternal 触发连接检测（内部API，异步）
// POST /api/internal/engines/:id/check-connection
// 用于其他模块在连接失败时通知System刷新状态
// 立即返回202 Accepted，实际检测在后台执行
func (h *EngineHandler) TriggerConnectionCheckInternal(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	// 异步检测，立即返回
	if err := h.engineService.AsyncCheckConnection(id); err != nil {
		commonapi.RespondError(c, http.StatusNotFound, err.Error())
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
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req UpdateConnectionStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 更新连接状态
	if err := h.engineService.UpdateConnectionStatus(id, req.ConnectionStatus, req.CheckMessage); err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	commonapi.RespondSuccess(c, gin.H{
		"success": true,
		"message": "连接状态已更新",
	})
}

// RegisterEngineRequest 引擎自注册请求
type RegisterEngineRequest struct {
	EngineType     string                 `json:"engine_type" binding:"required"`
	Name           string                 `json:"name" binding:"required"`
	Description    string                 `json:"description"`
	ConnectionInfo map[string]interface{} `json:"connection_info" binding:"required"`
	Capabilities   string                 `json:"capabilities"` // JSON 字符串
	IsBuiltin      bool                   `json:"is_builtin"`   // 是否为内置引擎（对所有租户可见）
}

// RegisterEngineInternal 内部API：引擎自注册（创建或更新引擎记录并触发连接检查）
// POST /api/internal/engines/register
// 用于工作流引擎启动时自动注册并触发连接检查
func (h *EngineHandler) RegisterEngineInternal(c *gin.Context) {
	var req RegisterEngineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	fmt.Printf("[RegisterEngine] 📥 收到引擎注册请求: type=%s, name=%s\n", req.EngineType, req.Name)

	// 1. 自动填充 host（从请求来源 IP，规范化回环地址）
	clientIP := c.ClientIP()
	// 将 IPv6/IPv4 回环地址统一规范化为 localhost
	if clientIP == "::1" || clientIP == "127.0.0.1" || clientIP == "localhost" {
		req.ConnectionInfo["host"] = "localhost"
	} else {
		req.ConnectionInfo["host"] = clientIP
	}
	fmt.Printf("[RegisterEngine] 🌐 自动填充 host: %s (来源 IP: %s)\n", req.ConnectionInfo["host"], clientIP)

	// 2. 查找是否已存在（engine_type + tenant_id IS NULL 表示平台级引擎）
	existingEngine, err := h.engineService.GetByEngineTypeAndTenant(req.EngineType, nil)

	var engine *models.Engine
	if err != nil {
		// 不存在 - 创建新记录
		fmt.Printf("[RegisterEngine] ➕ 引擎不存在，创建新记录 (is_builtin=%v)\n", req.IsBuiltin)
		newEngine := models.Engine{
			Name:             req.Name,
			EngineType:       req.EngineType,
			EngineCategory:   "extension", // 工作流引擎都是扩展引擎
			Description:      req.Description,
			ConnectionInfo:   req.ConnectionInfo,
			Capabilities:     &req.Capabilities,
			IsActive:         true,
			IsBuiltin:        req.IsBuiltin, // 使用请求中的 is_builtin 值
			TenantID:         nil,           // 平台级引擎
			ConnectionStatus: "unknown",
		}

		if err := h.engineService.CreateEngine(&newEngine); err != nil {
			fmt.Printf("[RegisterEngine] ❌ 创建引擎失败: %v\n", err)
			commonapi.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("创建引擎失败: %v", err))
			return
		}
		fmt.Printf("[RegisterEngine] ✅ 引擎创建成功，ID=%d\n", newEngine.ID)
		engine = &newEngine
	} else {
		// 已存在 - 更新记录
		fmt.Printf("[RegisterEngine] 🔄 引擎已存在 (ID=%d)，更新记录 (is_builtin=%v)\n", existingEngine.ID, req.IsBuiltin)
		existingEngine.Name = req.Name
		existingEngine.Description = req.Description
		existingEngine.ConnectionInfo = req.ConnectionInfo
		existingEngine.Capabilities = &req.Capabilities
		existingEngine.IsBuiltin = req.IsBuiltin // 更新 is_builtin 字段

		if err := h.engineService.UpdateEngine(existingEngine); err != nil {
			fmt.Printf("[RegisterEngine] ❌ 更新引擎失败: %v\n", err)
			commonapi.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("更新引擎失败: %v", err))
			return
		}
		fmt.Printf("[RegisterEngine] ✅ 引擎更新成功\n")
		engine = existingEngine
	}

	// 3. 异步触发连接检查
	fmt.Printf("[RegisterEngine] 🔍 触发异步连接测试...\n")
	if err := h.engineService.AsyncCheckConnection(engine.ID); err != nil {
		// 连接检查失败不影响注册成功，只记录日志
		fmt.Printf("[RegisterEngine] ⚠️  触发连接检查失败: %v\n", err)
	} else {
		fmt.Printf("[RegisterEngine] ✅ 异步连接测试已启动\n")
	}

	c.JSON(http.StatusAccepted, gin.H{
		"success":     true,
		"message":     "引擎注册成功，连接检查已启动",
		"engine_id":   engine.ID,
		"engine_type": req.EngineType,
	})
}

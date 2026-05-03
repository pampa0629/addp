package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	engineService *service.EngineService
	scanService   *service.ScanService
	taskService   *service.ScanTaskService
}

func NewHandler(engineService *service.EngineService, scanService *service.ScanService, taskService *service.ScanTaskService) *Handler {
	return &Handler{
		engineService: engineService,
		scanService:   scanService,
		taskService:   taskService,
	}
}

// handleServiceError 统一处理 Service 层错误，返回合适的 HTTP 状态码
func (h *Handler) handleServiceError(c *gin.Context, err error) {
	statusCode := metaErrors.HTTPStatusCode(err)
	message := metaErrors.ErrorMessage(err)
	c.JSON(statusCode, gin.H{"error": message})
}

// GetObjectMetadata 获取对象的元数据
// GET /api/meta/metadata/object
// Query params: engine_id, object_key
// @Summary 获取对象元数据 | Get object metadata
// @Description 获取指定对象存储文件的元数据信息 | Get metadata information for a specific object storage file
// @Tags Meta
// @Produce json
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param object_key query string true "对象存储路径 | Object storage path"
// @Success 200 {object} map[string]interface{} "对象元数据 | Object metadata"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "对象不存在 | Object not found"
// @Router /metadata/object [get]
// @Security BearerAuth
func (h *Handler) GetObjectMetadata(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	objectKey := c.Query("object_key")
	if objectKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing object_key"})
		return
	}

	item, err := h.scanService.GetObjectMetadata(tenantID, uint(engineID), objectKey)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetResources 获取资源列表及统计
// GET /api/meta/engines
// @Summary 获取引擎列表 | Get engine list
// @Description 获取当前租户的存储引擎列表及统计信息 | Get storage engine list with statistics for the current tenant
// @Tags Meta
// @Produce json
// @Success 200 {object} map[string]interface{} "引擎列表 | Engine list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines [get]
// @Security BearerAuth
func (h *Handler) GetEngines(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engines, err := h.engineService.GetEnginesWithStats(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, engines)
}

// ListObjectStorageNodes 分级列出对象存储节点
// @Summary 列出对象存储节点 | List object storage nodes
// @Description 分级列出对象存储的节点（Bucket/目录/文件）| Hierarchically list object storage nodes (Bucket/directory/file)
// @Tags Meta
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param path query string false "路径前缀 | Path prefix"
// @Success 200 {object} map[string]interface{} "节点列表 | Node list"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /object-storage/{engine_id}/nodes [get]
// @Security BearerAuth
func (h *Handler) ListObjectStorageNodes(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	path := c.Query("path")

	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	nodes, err := h.scanService.ListObjectStorageNodes(uint(engineID), tenantID, path, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, nodes)
}

// AutoScan 自动扫描所有未扫描的资源
// POST /api/meta/scan/auto
func (h *Handler) AutoScan(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	result, err := h.scanService.AutoScanUnscanned(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// CreateManualScanRun 创建异步扫描运行
func (h *Handler) CreateManualScanRun(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	var req models.ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, ok := extractBearerToken(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}

	run, err := h.taskService.CreateManualRun(c.Request.Context(), tenantID, userID, token, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, run)
}

// GetScanRun 获取执行详情（按 execution UUID）
func (h *Handler) GetScanRun(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	executionID := c.Param("run_id")
	if executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing execution_id"})
		return
	}

	exec, err := h.taskService.GetExecution(c.Request.Context(), executionID, int(tenantID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "execution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, exec)
}

// CancelScanRun 取消执行
func (h *Handler) CancelScanRun(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	executionID := c.Param("run_id")
	if executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing execution_id"})
		return
	}

	if err := h.taskService.CancelExecution(c.Request.Context(), executionID, int(tenantID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "execution cancelled"})
}

// ListScanRuns 列出执行记录（从 common.task_executions 查询）
func (h *Handler) ListScanRuns(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	var err error

	var taskID *int
	if taskIDStr := c.Query("task_id"); taskIDStr != "" {
		val, parseErr := strconv.Atoi(taskIDStr)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
			return
		}
		taskID = &val
	}

	status := strings.TrimSpace(c.Query("status"))
	triggerType := strings.TrimSpace(c.Query("trigger_type"))

	page := 1
	if pageStr := c.Query("page"); pageStr != "" {
		if page, err = strconv.Atoi(pageStr); err != nil || page <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page"})
			return
		}
	}

	pageSize := 20
	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if pageSize, err = strconv.Atoi(pageSizeStr); err != nil || pageSize <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_size"})
			return
		}
	}

	executions, total, err := h.taskService.ListExecutions(c.Request.Context(), int(tenantID), taskID, status, triggerType, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	totalPages := int64(0)
	if pageSize > 0 {
		totalPages = (total + int64(pageSize) - 1) / int64(pageSize)
	}

	c.JSON(http.StatusOK, gin.H{
		"items":       executions,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// CreateScanTask 创建扫描任务
func (h *Handler) CreateScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	var req models.ScanTaskUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.taskService.CreateTask(c.Request.Context(), tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, task)
}

// UpdateScanTask 更新任务
func (h *Handler) UpdateScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	taskIDStr := c.Param("task_id")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
		return
	}

	var req models.ScanTaskUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.taskService.UpdateTask(c.Request.Context(), tenantID, uint(taskID), userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, task)
}

// DeleteScanTask 删除任务
func (h *Handler) DeleteScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)

	taskIDStr := c.Param("task_id")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
		return
	}

	if err := h.taskService.DeleteTask(c.Request.Context(), tenantID, uint(taskID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "task deleted"})
}

// TriggerScanTask 手动触发任务
func (h *Handler) TriggerScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	taskIDStr := c.Param("task_id")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
		return
	}

	run, err := h.taskService.TriggerTaskNow(c.Request.Context(), tenantID, uint(taskID), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, run)
}

// ListScanTasks 列出台账
func (h *Handler) ListScanTasks(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)

	tasks, err := h.taskService.ListTasks(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tasks)
}

// ScanEngine 扫描指定引擎
// POST /api/meta/scan/engine
func (h *Handler) ScanEngine(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	var req models.ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 提取JWT token（服务间调用时 token 可为空，因为使用 X-Internal-API-Key 认证）
	token := c.GetHeader("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}
	// 注意：不强制要求 token，服务间调用通过 X-Internal-API-Key 认证（middleware 已处理）
	// tenantID 已从 middleware 提取（line 494），用于多租户数据过滤

	result, err := h.scanService.ScanEngine(req.EngineID, tenantID, req.Namespaces, req.ObjectPaths, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ExtractObjectMetadata 按需提取对象的深度元数据
// POST /api/meta/metadata/extract
// Query params: engine_id, object_key
// Body: 对象的二进制内容
func (h *Handler) ExtractObjectMetadata(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	objectKey := c.Query("object_key")
	if objectKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing object_key"})
		return
	}

	// 获取Authorization token（用于访问System API）
	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	// 从请求体读取对象内容
	objectReader := c.Request.Body
	defer c.Request.Body.Close()

	// 调用扫描服务提取元数据
	metadata, err := h.scanService.ExtractObjectMetadataOnDemand(tenantID, uint(engineID), objectKey, token, objectReader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, metadata)
}

func extractBearerToken(c *gin.Context) (string, bool) {
	token := c.GetHeader("Authorization")
	if token == "" {
		return "", false
	}
	if len(token) > 7 && strings.HasPrefix(token, "Bearer ") {
		token = token[7:]
	}
	return token, token != ""
}

// ListEngineItems 获取引擎下已扫描的数据项列表。
// GET /api/v1/meta/engines/:engine_id/items?namespace=public
func (h *Handler) ListEngineItems(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	namespace := c.Query("namespace")
	var items []models.MetaItemLite
	if namespace != "" {
		items, err = h.scanService.ListItemsByNamespace(uint(engineID), tenantID, namespace)
	} else {
		items, err = h.scanService.ListItemsByEngine(uint(engineID), tenantID)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, items)
}

// GetItemFieldsByName 按 engine/namespace/name 获取数据项字段。
// GET /api/v1/meta/engines/:engine_id/items/fields?namespace=public&name=users
func (h *Handler) GetItemFieldsByName(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Param("engine_id")
	namespace := c.Query("namespace")
	itemName := c.Query("name")
	includeDetails := c.Query("include_details")

	if itemName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing name"})
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	if includeDetails == "true" || includeDetails == "1" {
		fields, err := h.scanService.GetItemFieldDetailsByName(uint(engineID), namespace, itemName, tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, fields)
		return
	}

	names, err := h.scanService.GetItemFieldNames(uint(engineID), namespace, itemName, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, names)
}

// GetItemFieldsByID 按 item_id 获取数据项字段。
// GET /api/v1/meta/items/:item_id/fields
func (h *Handler) GetItemFieldsByID(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	itemIDStr := c.Param("item_id")
	itemID, err := strconv.ParseUint(itemIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item_id"})
		return
	}

	fields, err := h.scanService.GetItemFieldDetailsByID(tenantID, uint(itemID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, fields)
}

// ClearResourceCache 清除资源缓存
// DELETE /api/meta/cache/engines/:engine_id
func (h *Handler) ClearResourceCache(c *gin.Context) {
	engineIDStr := c.Param("engine_id")
	if engineIDStr == "all" {
		// 清除所有缓存
		h.engineService.ClearCache()
		c.JSON(http.StatusOK, gin.H{
			"message": "已清除所有资源缓存",
		})
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	h.engineService.ClearEngineCache(uint(engineID))
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("已清除资源 %d 的缓存", engineID),
	})
}

// RefreshResourceCache 刷新资源缓存（先清除再重新加载）
// POST /api/meta/cache/refresh
func (h *Handler) RefreshResourceCache(c *gin.Context) {
	h.engineService.ClearCache()
	if err := h.engineService.PreloadResources(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("刷新缓存失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "资源缓存已刷新",
	})
}

// ========== 新增：用于 Manager 模块的元数据查询接口 ==========

// GetMetadataTree 获取资源的完整元数据树
// GET /api/meta/engines/:engine_id/tree
func (h *Handler) GetMetadataTree(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	tree, err := h.scanService.GetMetadataTree(tenantID, uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tree)
}

// GetMetaNodeByID 获取单个节点详情
// GET /api/meta/nodes/:node_id
func (h *Handler) GetMetaNodeByID(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	nodeIDStr := c.Param("node_id")
	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node_id"})
		return
	}

	node, err := h.scanService.GetMetaNodeByID(tenantID, uint(nodeID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, node)
}

// GetNodeChildren 获取节点的子节点
// GET /api/meta/nodes/:node_id/children
func (h *Handler) GetNodeChildren(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	nodeIDStr := c.Param("node_id")
	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node_id"})
		return
	}

	nodes, err := h.scanService.GetNodeChildren(tenantID, uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, nodes)
}

// GetNodeItems 获取节点下的项目
// GET /api/meta/nodes/:node_id/items
func (h *Handler) GetNodeItems(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	nodeIDStr := c.Param("node_id")
	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node_id"})
		return
	}

	items, err := h.scanService.GetNodeItems(tenantID, uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, items)
}

// QueryNodeByPath 按路径查询节点
// GET /api/meta/nodes/by-path?engine_id=X&path=Y
func (h *Handler) QueryNodeByPath(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing path parameter"})
		return
	}

	node, err := h.scanService.GetNodeByPath(tenantID, uint(engineID), path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, node)
}

// QueryItemByPath 按路径查询项目（对象存储）
// GET /api/meta/items/by-path?engine_id=X&bucket=Y&path=Z
func (h *Handler) QueryItemByPath(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	bucket := c.Query("bucket")
	if bucket == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing bucket parameter"})
		return
	}

	path := c.Query("path")

	item, err := h.scanService.GetItemByPath(tenantID, uint(engineID), bucket, path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetItemByID 按 ID 查询 MetaItem
// GET /api/meta/items/:item_id
func (h *Handler) GetItemByID(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	itemIDStr := c.Param("item_id")
	itemID, err := strconv.ParseUint(itemIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item_id"})
		return
	}

	item, err := h.scanService.GetItemByID(tenantID, uint(itemID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetItemSpatialMetadataByName 按 engine/namespace/name 获取数据项空间元数据。
// GET /api/v1/meta/engines/:engine_id/items/spatial?namespace=public&name=users
func (h *Handler) GetItemSpatialMetadataByName(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	namespace := c.Query("namespace")
	itemName := c.Query("name")
	if itemName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing name parameter"})
		return
	}

	spatialMeta, err := h.scanService.GetItemSpatialMetadataByName(tenantID, uint(engineID), namespace, itemName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, spatialMeta)
}

// GetItemSpatialMetadataByID 按 item_id 获取数据项空间元数据。
// GET /api/v1/meta/items/:item_id/spatial
func (h *Handler) GetItemSpatialMetadataByID(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	itemIDStr := c.Param("item_id")
	itemID, err := strconv.ParseUint(itemIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item_id"})
		return
	}

	spatialMeta, err := h.scanService.GetItemSpatialMetadataByID(tenantID, uint(itemID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, spatialMeta)
}

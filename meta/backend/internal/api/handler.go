package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	engineService *service.EngineService
	scanService     *service.ScanService
	taskService     *service.ScanTaskService
}

func NewHandler(engineService *service.EngineService, scanService *service.ScanService, taskService *service.ScanTaskService) *Handler {
	return &Handler{
		engineService: engineService,
		scanService:     scanService,
		taskService:     taskService,
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

	c.JSON(http.StatusOK, gin.H{"data": item})
}

// GetResources 获取资源列表及统计
// GET /api/meta/engines
func (h *Handler) GetEngines(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engines, err := h.engineService.GetEnginesWithStats(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": engines})
}

// GetSchemas 获取资源的Schema列表
// GET /api/meta/schemas/:engine_id
func (h *Handler) GetSchemas(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	schemas, err := h.scanService.GetSchemasByResource(uint(engineID), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": schemas})
}

// ListAvailableSchemas 列出资源中可用的Schema（从数据库实时查询）
// GET /api/meta/schemas/:engine_id/available
func (h *Handler) ListAvailableSchemas(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	// 从请求头中提取JWT token，传递给System API
	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}
	// 去掉 "Bearer " 前缀
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	schemas, err := h.scanService.ListAvailableSchemas(uint(engineID), tenantID, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": schemas})
}

// ListObjectStorageNodes 分级列出对象存储节点
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

	c.JSON(http.StatusOK, gin.H{"data": nodes})
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

	c.JSON(http.StatusOK, gin.H{"data": run})
}

// GetScanRun 获取运行进度
func (h *Handler) GetScanRun(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	runIDStr := c.Param("run_id")
	runID, err := strconv.ParseUint(runIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_id"})
		return
	}

	run, err := h.taskService.GetRun(uint(runID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if run.TenantID != tenantID {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": run})
}

// ListScanRuns 列出运行任务
func (h *Handler) ListScanRuns(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)

	var (
		taskID     *uint
		resourceID *uint
		err        error
	)

	if taskIDStr := c.Query("task_id"); taskIDStr != "" {
		val, parseErr := strconv.ParseUint(taskIDStr, 10, 32)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
			return
		}
		taskID = new(uint)
		*taskID = uint(val)
	}

	if engineIDStr := c.Query("engine_id"); engineIDStr != "" {
		val, parseErr := strconv.ParseUint(engineIDStr, 10, 32)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
			return
		}
		resourceID = new(uint)
		*resourceID = uint(val)
	}

	status := c.Query("status")
	storageType := c.Query("storage_type")
	triggerType := c.Query("trigger_type")

	pageSize := 20
	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if pageSize, err = strconv.Atoi(pageSizeStr); err != nil || pageSize <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_size"})
			return
		}
	}

	limit := pageSize
	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err = strconv.Atoi(limitStr); err != nil || limit <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
	}

	page := 1
	if pageStr := c.Query("page"); pageStr != "" {
		if page, err = strconv.Atoi(pageStr); err != nil || page <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page"})
			return
		}
	}

	offset := (page - 1) * limit
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if offset, err = strconv.Atoi(offsetStr); err != nil || offset < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset"})
			return
		}
	}

	var startedAfter *time.Time
	if from := strings.TrimSpace(c.Query("started_after")); from != "" {
		if parsed, parseErr := time.ParseInLocation("2006-01-02 15:04:05", from, time.Local); parseErr == nil {
			startedAfter = &parsed
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid started_after"})
			return
		}
	}

	var startedBefore *time.Time
	if to := strings.TrimSpace(c.Query("started_before")); to != "" {
		if parsed, parseErr := time.ParseInLocation("2006-01-02 15:04:05", to, time.Local); parseErr == nil {
			startedBefore = &parsed
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid started_before"})
			return
		}
	}

	options := &service.ListRunsOptions{
		TaskID:        taskID,
		EngineID:    resourceID,
		Status:        strings.TrimSpace(status),
		TriggerType:   strings.TrimSpace(triggerType),
		StorageType:   strings.TrimSpace(storageType),
		StartedAfter:  startedAfter,
		StartedBefore: startedBefore,
		Limit:         limit,
		Offset:        offset,
	}

	runs, total, err := h.taskService.ListRuns(tenantID, options)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  runs,
		"total": total,
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

	c.JSON(http.StatusOK, gin.H{"data": task})
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

	c.JSON(http.StatusOK, gin.H{"data": task})
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

	c.JSON(http.StatusOK, gin.H{"data": run})
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

	c.JSON(http.StatusOK, gin.H{"data": tasks})
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

	// 提取JWT token
	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	result, err := h.scanService.ScanEngine(req.EngineID, tenantID, req.SchemaNames, req.ObjectPaths, token)
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

	c.JSON(http.StatusOK, gin.H{
		"data":    metadata,
		"message": "元数据提取成功",
	})
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

// GetTables 获取资源的表列表（用于Transfer模块字段选择）
// GET /api/metadata/tables?engine_id=1
func (h *Handler) GetTables(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	if engineIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing engine_id"})
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	// 获取该资源下的所有表
	tables, err := h.scanService.GetTablesByResource(uint(engineID), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 调试：打印前3个表的信息到标准输出
	fmt.Printf("[GetTables] Found %d tables\n", len(tables))
	for i, table := range tables {
		if i < 3 {
			fmt.Printf("[GetTables] Table %d: Name=%s, FullName='%s'\n", i, table.Name, table.FullName)
		}
	}

	// 返回表名列表（优先使用 FullName，如果为空则使用 Name）
	tableNames := make([]string, len(tables))
	for i, table := range tables {
		// FullName 包含 schema 前缀（如 sales.orders），适用于 PostgreSQL
		// 如果 FullName 为空，则回退到 Name（用于向后兼容）
		if table.FullName != "" {
			tableNames[i] = table.FullName
		} else {
			tableNames[i] = table.Name
		}
	}

	c.JSON(http.StatusOK, tableNames)
}

// GetTableFields 获取表的字段列表（用于Transfer模块字段映射）
// GET /api/metadata/fields?engine_id=1&table_name=users
func (h *Handler) GetTableFields(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	tableName := c.Query("table_name")
	includeDetails := c.Query("include_details")

	if engineIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing engine_id"})
		return
	}
	if tableName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing table_name"})
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	// 获取表字段信息
	if includeDetails == "true" || includeDetails == "1" {
		fields, err := h.scanService.GetTableFieldDetails(uint(engineID), tableName, tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, fields)
		return
	}

	names, err := h.scanService.GetTableFields(uint(engineID), tableName, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, names)
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

	c.JSON(http.StatusOK, gin.H{"data": tree})
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

	c.JSON(http.StatusOK, gin.H{"data": node})
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

	c.JSON(http.StatusOK, gin.H{"data": nodes})
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

	c.JSON(http.StatusOK, gin.H{"data": items})
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

	c.JSON(http.StatusOK, gin.H{"data": node})
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

	c.JSON(http.StatusOK, gin.H{"data": item})
}

// GetTableSpatialMetadata 获取表的空间元数据（MVT专用）
// GET /api/meta/metadata/tables/spatial?engine_id=X&schema=Y&table=Z
func (h *Handler) GetTableSpatialMetadata(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	schema := c.Query("schema")
	if schema == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing schema parameter"})
		return
	}

	table := c.Query("table")
	if table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing table parameter"})
		return
	}

	spatialMeta, err := h.scanService.GetTableSpatialMetadata(tenantID, uint(engineID), schema, table)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": spatialMeta})
}

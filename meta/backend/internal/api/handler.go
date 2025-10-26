package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/addp/meta/internal/middleware"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	resourceService *service.ResourceService
	scanService     *service.ScanServiceNew
	taskService     *service.ScanTaskService
}

func NewHandler(resourceService *service.ResourceService, scanService *service.ScanServiceNew, taskService *service.ScanTaskService) *Handler {
	return &Handler{
		resourceService: resourceService,
		scanService:     scanService,
		taskService:     taskService,
	}
}

// GetObjectMetadata 获取对象的元数据
// GET /api/meta/metadata/object
// Query params: resource_id, object_key
func (h *Handler) GetObjectMetadata(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Query("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	objectKey := c.Query("object_key")
	if objectKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing object_key"})
		return
	}

	item, err := h.scanService.GetObjectMetadata(tenantID, uint(resourceID), objectKey)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": item})
}

// GetResources 获取资源列表及统计
// GET /api/meta/resources
func (h *Handler) GetResources(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resources, err := h.resourceService.GetResourcesWithStats(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": resources})
}

// GetSchemas 获取资源的Schema列表
// GET /api/meta/schemas/:resource_id
func (h *Handler) GetSchemas(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Param("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	schemas, err := h.scanService.GetSchemasByResource(uint(resourceID), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": schemas})
}

// ListAvailableSchemas 列出资源中可用的Schema（从数据库实时查询）
// GET /api/meta/schemas/:resource_id/available
func (h *Handler) ListAvailableSchemas(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Param("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
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

	schemas, err := h.scanService.ListAvailableSchemas(uint(resourceID), tenantID, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": schemas})
}

// ListObjectStorageNodes 分级列出对象存储节点
func (h *Handler) ListObjectStorageNodes(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Param("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
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

	nodes, err := h.scanService.ListObjectStorageNodes(uint(resourceID), tenantID, path, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": nodes})
}

// AutoScan 自动扫描所有未扫描的资源
// POST /api/meta/scan/auto
func (h *Handler) AutoScan(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

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

	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)

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

	tenantID := middleware.GetTenantID(c)
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

	tenantID := middleware.GetTenantID(c)

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

	if resourceIDStr := c.Query("resource_id"); resourceIDStr != "" {
		val, parseErr := strconv.ParseUint(resourceIDStr, 10, 32)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
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
		ResourceID:    resourceID,
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

	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)

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

	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)

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

	tenantID := middleware.GetTenantID(c)

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

	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)

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

	tenantID := middleware.GetTenantID(c)

	tasks, err := h.taskService.ListTasks(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tasks})
}

// ScanResource 扫描指定资源
// POST /api/meta/scan/resource
func (h *Handler) ScanResource(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

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

	result, err := h.scanService.ScanResource(req.ResourceID, tenantID, req.SchemaNames, req.ObjectPaths, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ExtractObjectMetadata 按需提取对象的深度元数据
// POST /api/meta/metadata/extract
// Query params: resource_id, object_key
// Body: 对象的二进制内容
func (h *Handler) ExtractObjectMetadata(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Query("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
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
	metadata, err := h.scanService.ExtractObjectMetadataOnDemand(tenantID, uint(resourceID), objectKey, token, objectReader)
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
// GET /api/metadata/tables?resource_id=1
func (h *Handler) GetTables(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Query("resource_id")
	if resourceIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing resource_id"})
		return
	}

	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	// 获取该资源下的所有表
	tables, err := h.scanService.GetTablesByResource(uint(resourceID), tenantID)
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
// GET /api/metadata/fields?resource_id=1&table_name=users
func (h *Handler) GetTableFields(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Query("resource_id")
	tableName := c.Query("table_name")
	includeDetails := c.Query("include_details")

	if resourceIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing resource_id"})
		return
	}
	if tableName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing table_name"})
		return
	}

	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	// 获取表字段信息
	if includeDetails == "true" || includeDetails == "1" {
		fields, err := h.scanService.GetTableFieldDetails(uint(resourceID), tableName, tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, fields)
		return
	}

	names, err := h.scanService.GetTableFields(uint(resourceID), tableName, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, names)
}

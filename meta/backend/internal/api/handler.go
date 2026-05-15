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

// GetStats 获取元数据统计
// @Summary 获取元数据统计 | Get metadata stats
// @Description 获取当前租户的元数据项总数 | Get metadata item count for current tenant
// @Tags Meta
// @Produce json
// @Success 200 {object} map[string]interface{} "统计信息 | Stats"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /stats [get]
// @Security BearerAuth
func (h *Handler) GetStats(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	itemCount, err := h.scanService.CountItems(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"total": itemCount})
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

// AutoScan 自动扫描所有未扫描的资源
// @Summary 自动扫描未扫描资源 | Auto scan unscanned resources
// @Description 扫描当前租户下尚未完成元数据扫描的资源 | Scan resources that have not been scanned for current tenant
// @Tags Meta Scan
// @Produce json
// @Success 200 {object} map[string]interface{} "扫描结果 | Scan result"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/auto [post]
// @Security BearerAuth
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
// @Summary 创建手动扫描运行 | Create manual scan run
// @Description 创建一个异步手动扫描执行记录并入队 | Create and enqueue a manual metadata scan run
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param request body models.ScanRequest true "扫描请求 | Scan request"
// @Success 201 {object} map[string]interface{} "执行记录 | Execution"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/run/manual [post]
// @Security BearerAuth
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
	if req.EngineID == 0 && req.NodeID == 0 && req.ItemID == 0 && len(req.Targets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "engine_id, node_id, item_id or targets is required"})
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
// @Summary 获取扫描运行详情 | Get scan run
// @Description 按执行 UUID 获取扫描运行详情 | Get scan execution by UUID
// @Tags Meta Scan
// @Produce json
// @Param run_id path string true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{} "执行详情 | Execution detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "执行不存在 | Execution not found"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/runs/{run_id} [get]
// @Security BearerAuth
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
// @Summary 取消扫描运行 | Cancel scan run
// @Description 按执行 UUID 取消扫描运行 | Cancel scan execution by UUID
// @Tags Meta Scan
// @Produce json
// @Param run_id path string true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{} "取消结果 | Cancel result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/runs/{run_id}/cancel [post]
// @Security BearerAuth
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
// @Summary 列出扫描运行 | List scan runs
// @Description 分页查询当前租户的扫描运行记录 | List scan executions for current tenant
// @Tags Meta Scan
// @Produce json
// @Param task_id query int false "任务ID | Task ID"
// @Param status query string false "执行状态 | Execution status"
// @Param trigger_type query string false "触发类型 | Trigger type"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} map[string]interface{} "分页执行记录 | Paged executions"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/runs [get]
// @Security BearerAuth
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
// @Summary 创建扫描任务 | Create scan task
// @Description 创建一个定时或手动扫描任务 | Create a scheduled or manual scan task
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param request body models.ScanTaskUpsertRequest true "扫描任务请求 | Scan task request"
// @Success 201 {object} map[string]interface{} "扫描任务 | Scan task"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks [post]
// @Security BearerAuth
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
// @Summary 更新扫描任务 | Update scan task
// @Description 更新扫描任务配置 | Update scan task configuration
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param task_id path int true "任务ID | Task ID"
// @Param request body models.ScanTaskUpsertRequest true "扫描任务请求 | Scan task request"
// @Success 200 {object} map[string]interface{} "扫描任务 | Scan task"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/{task_id} [put]
// @Security BearerAuth
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
// @Summary 删除扫描任务 | Delete scan task
// @Description 删除指定扫描任务 | Delete scan task by ID
// @Tags Meta Scan
// @Produce json
// @Param task_id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除结果 | Delete result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/{task_id} [delete]
// @Security BearerAuth
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
// @Summary 触发扫描任务 | Trigger scan task
// @Description 立即触发指定扫描任务 | Trigger scan task immediately
// @Tags Meta Scan
// @Produce json
// @Param task_id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "执行记录 | Execution"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/{task_id}/trigger [post]
// @Security BearerAuth
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

// ListScanTasks 列出扫描任务
// @Summary 列出扫描任务 | List scan tasks
// @Description 列出当前租户的扫描任务 | List scan tasks for current tenant
// @Tags Meta Scan
// @Produce json
// @Success 200 {array} map[string]interface{} "扫描任务列表 | Scan tasks"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks [get]
// @Security BearerAuth
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
// @Summary 扫描指定引擎 | Scan engine
// @Description 对指定引擎执行元数据扫描 | Execute metadata scan for an engine
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param request body models.ScanRequest true "扫描请求 | Scan request"
// @Success 200 {object} models.ScanResponse "扫描结果 | Scan result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/engine [post]
// @Security BearerAuth
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

	scanDepth := req.ScanDepth
	if scanDepth == "" {
		scanDepth = "basic"
	}
	result, err := h.scanService.ScanEngineWithOptions(service.ScanOptions{
		EngineID:    req.EngineID,
		TenantID:    tenantID,
		Namespaces:  req.Namespaces,
		ObjectPaths: req.ObjectPaths,
		Token:       token,
		ScanDepth:   scanDepth,
		Force:       req.Force,
		NodeID:      req.NodeID,
		ItemID:      req.ItemID,
		Targets:     req.Targets,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ExtractObjectMetadata 按需提取对象的深度元数据
// @Summary 提取对象元数据 | Extract object metadata
// @Description 按需读取请求体内容并提取对象深度元数据 | Extract object metadata from request body
// @Tags Meta
// @Accept octet-stream
// @Produce json
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param object_key query string true "对象路径 | Object key"
// @Success 200 {object} map[string]interface{} "对象元数据 | Object metadata"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /metadata/extract [post]
// @Security BearerAuth
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

// BuildObjectContentIndex 按需建立对象内容索引
// @Summary 建立对象内容索引 | Build object content index
// @Description 按需读取请求体内容并建立对象内容索引 | Build object content index from request body
// @Tags Meta
// @Accept octet-stream
// @Produce json
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param object_key query string true "对象路径 | Object key"
// @Success 200 {object} map[string]interface{} "对象 attributes | Object attributes"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /metadata/content-index [post]
// @Security BearerAuth
func (h *Handler) BuildObjectContentIndex(c *gin.Context) {
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

	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}

	attrs, err := h.scanService.BuildObjectContentIndexOnDemand(tenantID, uint(engineID), objectKey, c.Request.Body)
	_ = c.Request.Body.Close()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"attributes": attrs}})
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

func (h *Handler) effectiveTenantIDForEngine(c *gin.Context, engineID uint) (uint, error) {
	tenantID := commonAuth.GetTenantID(c)
	if tenantID != 0 {
		return tenantID, nil
	}

	token, _ := extractBearerToken(c)
	engine, err := h.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return 0, err
	}
	if engine.TenantID != nil && *engine.TenantID > 0 {
		return *engine.TenantID, nil
	}
	return tenantID, nil
}

// ListEngineItems 获取引擎下已扫描的数据项列表。
// @Summary 列出引擎数据项 | List engine items
// @Description 获取指定引擎下已扫描的数据项，可按命名空间过滤 | List scanned metadata items for an engine
// @Tags Meta Query
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param namespace query string false "命名空间 | Namespace"
// @Success 200 {array} models.MetaItemLite "数据项列表 | Items"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines/{engine_id}/items [get]
// @Security BearerAuth
func (h *Handler) ListEngineItems(c *gin.Context) {
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}
	tenantID, err := h.effectiveTenantIDForEngine(c, uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Summary 按名称获取数据项字段 | Get item fields by name
// @Description 按引擎、命名空间和数据项名称获取字段信息 | Get item fields by engine, namespace and item name
// @Tags Meta Query
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param namespace query string false "命名空间 | Namespace"
// @Param name query string true "数据项名称 | Item name"
// @Param include_details query bool false "是否返回详细字段 | Include details"
// @Success 200 {object} map[string]interface{} "字段信息 | Fields"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines/{engine_id}/items/fields [get]
// @Security BearerAuth
func (h *Handler) GetItemFieldsByName(c *gin.Context) {
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
	tenantID, err := h.effectiveTenantIDForEngine(c, uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Summary 按 ID 获取数据项字段 | Get item fields by ID
// @Description 按数据项 ID 获取字段详情 | Get item field details by item ID
// @Tags Meta Query
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Success 200 {object} map[string]interface{} "字段详情 | Field details"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "数据项不存在 | Item not found"
// @Router /items/{item_id}/fields [get]
// @Security BearerAuth
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
// @Summary 清除引擎缓存 | Clear engine cache
// @Description 清除指定引擎资源缓存，engine_id 为 all 时清除全部缓存 | Clear engine resource cache
// @Tags Meta Cache
// @Produce json
// @Param engine_id path string true "存储引擎ID或all | Engine ID or all"
// @Success 200 {object} map[string]interface{} "清除结果 | Clear result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /cache/engines/{engine_id} [delete]
// @Security BearerAuth
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
// @Summary 刷新资源缓存 | Refresh resource cache
// @Description 清除并重新预加载资源缓存 | Clear and preload resource cache
// @Tags Meta Cache
// @Produce json
// @Success 200 {object} map[string]interface{} "刷新结果 | Refresh result"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /cache/refresh [post]
// @Security BearerAuth
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
// @Summary 获取元数据树 | Get metadata tree
// @Description 获取指定引擎的完整元数据树 | Get metadata tree for an engine
// @Tags Meta Query
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Success 200 {object} models.MetadataTreeResponse "元数据树 | Metadata tree"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines/{engine_id}/tree [get]
// @Security BearerAuth
func (h *Handler) GetMetadataTree(c *gin.Context) {
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}
	tenantID, err := h.effectiveTenantIDForEngine(c, uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Summary 获取节点详情 | Get node detail
// @Description 按节点 ID 获取元数据节点详情 | Get metadata node detail by ID
// @Tags Meta Query
// @Produce json
// @Param node_id path int true "节点ID | Node ID"
// @Success 200 {object} models.MetaNodeLite "节点详情 | Node detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "节点不存在 | Node not found"
// @Router /nodes/{node_id} [get]
// @Security BearerAuth
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
// @Summary 获取子节点 | Get node children
// @Description 获取指定节点的直接子节点 | Get direct children of a metadata node
// @Tags Meta Query
// @Produce json
// @Param node_id path int true "节点ID | Node ID"
// @Success 200 {array} models.MetaNodeLite "子节点列表 | Children"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /nodes/{node_id}/children [get]
// @Security BearerAuth
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

// GetNodeItems 获取节点下的数据项
// @Summary 获取节点数据项 | Get node items
// @Description 获取指定节点下的数据项 | Get metadata items under a node
// @Tags Meta Query
// @Produce json
// @Param node_id path int true "节点ID | Node ID"
// @Success 200 {array} models.MetaItemLite "数据项列表 | Items"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /nodes/{node_id}/items [get]
// @Security BearerAuth
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
// @Summary 按路径查询节点 | Query node by path
// @Description 按引擎和路径查询元数据节点 | Query metadata node by engine and path
// @Tags Meta Query
// @Produce json
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param path query string true "节点路径 | Node path"
// @Success 200 {object} models.MetaNodeLite "节点详情 | Node detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "节点不存在 | Node not found"
// @Router /nodes/by-path [get]
// @Security BearerAuth
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

// QueryItemByPath 按路径查询数据项
// @Summary 按路径查询数据项 | Query item by path
// @Description 按引擎、bucket 和路径查询数据项 | Query metadata item by engine, bucket and path
// @Tags Meta Query
// @Produce json
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param bucket query string true "Bucket或顶层命名空间 | Bucket or namespace"
// @Param path query string false "数据项路径 | Item path"
// @Success 200 {object} models.MetaItemLite "数据项详情 | Item detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "数据项不存在 | Item not found"
// @Router /items/by-path [get]
// @Security BearerAuth
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
// @Summary 获取数据项详情 | Get item detail
// @Description 按数据项 ID 获取元数据项详情 | Get metadata item detail by ID
// @Tags Meta Query
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Success 200 {object} models.MetaItemLite "数据项详情 | Item detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "数据项不存在 | Item not found"
// @Router /items/{item_id} [get]
// @Security BearerAuth
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
// @Summary 按名称获取空间元数据 | Get spatial metadata by name
// @Description 按引擎、命名空间和数据项名称获取空间元数据 | Get spatial metadata by engine, namespace and item name
// @Tags Meta Query
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param namespace query string false "命名空间 | Namespace"
// @Param name query string true "数据项名称 | Item name"
// @Success 200 {object} models.SpatialMetadataResponse "空间元数据 | Spatial metadata"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "空间元数据不存在 | Spatial metadata not found"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines/{engine_id}/items/spatial [get]
// @Security BearerAuth
func (h *Handler) GetItemSpatialMetadataByName(c *gin.Context) {
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}
	tenantID, err := h.effectiveTenantIDForEngine(c, uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Summary 按 ID 获取空间元数据 | Get spatial metadata by ID
// @Description 按数据项 ID 获取空间元数据 | Get spatial metadata by item ID
// @Tags Meta Query
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Success 200 {object} models.SpatialMetadataResponse "空间元数据 | Spatial metadata"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "空间元数据不存在 | Spatial metadata not found"
// @Router /items/{item_id}/spatial [get]
// @Security BearerAuth
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

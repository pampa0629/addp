package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type MetadataHandler struct {
	metadataService *service.MetadataService
}

func NewMetadataHandler(metadataService *service.MetadataService) *MetadataHandler {
	return &MetadataHandler{
		metadataService: metadataService,
	}
}

// ListScanTasks 列出指定资源下的扫描任务
// @Summary 列出扫描任务 | List scan tasks
// @Description 列出指定存储引擎下的所有元数据扫描任务 | List all metadata scan tasks for a specific engine
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Success 200 {object} map[string]interface{} "扫描任务列表 | Scan task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Security BearerAuth
func (h *MetadataHandler) ListScanTasks(c *gin.Context) {
	engineID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	authHeader, ok := extractAuthHeader(c)
	if !ok {
		return
	}

	tasks, err := h.metadataService.ListScanTasks(c.Request.Context(), engineID, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tasks})
}

// CreateScanTask 创建新的扫描任务
// @Summary 创建扫描任务 | Create scan task
// @Description 为指定存储引擎创建新的元数据扫描任务 | Create a new metadata scan task for a specific engine
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param body body models.MetaScanTaskRequest true "扫描任务配置 | Scan task configuration"
// @Success 200 {object} map[string]interface{} "创建的扫描任务 | Created scan task"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Security BearerAuth
func (h *MetadataHandler) CreateScanTask(c *gin.Context) {
	engineID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	authHeader, ok := extractAuthHeader(c)
	if !ok {
		return
	}

	var req models.MetaScanTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.metadataService.CreateScanTask(c.Request.Context(), engineID, &req, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": task})
}

// UpdateScanTask 更新扫描任务
// @Summary 更新扫描任务 | Update scan task
// @Description 更新指定存储引擎的元数据扫描任务配置 | Update metadata scan task configuration for a specific engine
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param task_id path int true "扫描任务ID | Scan task ID"
// @Param body body models.MetaScanTaskRequest true "扫描任务配置 | Scan task configuration"
// @Success 200 {object} map[string]interface{} "更新后的扫描任务 | Updated scan task"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Security BearerAuth
func (h *MetadataHandler) UpdateScanTask(c *gin.Context) {
	engineID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	taskID, ok := parseUintParam(c, "task_id")
	if !ok {
		return
	}

	authHeader, ok := extractAuthHeader(c)
	if !ok {
		return
	}

	var req models.MetaScanTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.metadataService.UpdateScanTask(c.Request.Context(), engineID, taskID, &req, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": task})
}

// DeleteScanTask 删除扫描任务
// @Summary 删除扫描任务 | Delete scan task
// @Description 删除指定的元数据扫描任务 | Delete a specific metadata scan task
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param task_id path int true "扫描任务ID | Scan task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Security BearerAuth
func (h *MetadataHandler) DeleteScanTask(c *gin.Context) {
	_, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	taskID, ok := parseUintParam(c, "task_id")
	if !ok {
		return
	}

	authHeader, ok := extractAuthHeader(c)
	if !ok {
		return
	}

	if err := h.metadataService.DeleteScanTask(c.Request.Context(), taskID, authHeader); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "task deleted"})
}

// TriggerScanTask 立即触发扫描任务
// @Summary 触发扫描任务 | Trigger scan task
// @Description 立即触发指定的元数据扫描任务执行 | Immediately trigger a specific metadata scan task
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param task_id path int true "扫描任务ID | Scan task ID"
// @Success 200 {object} map[string]interface{} "触发的扫描运行记录 | Triggered scan run"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Security BearerAuth
func (h *MetadataHandler) TriggerScanTask(c *gin.Context) {
	_, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	taskID, ok := parseUintParam(c, "task_id")
	if !ok {
		return
	}

	authHeader, ok := extractAuthHeader(c)
	if !ok {
		return
	}

	run, err := h.metadataService.TriggerScanTask(c.Request.Context(), taskID, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": run})
}

// ListScanRuns 列出资源的扫描运行记录
// @Summary 列出扫描运行记录 | List scan runs
// @Description 列出指定存储引擎的元数据扫描运行历史记录 | List metadata scan run history for a specific engine
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param task_id query int false "扫描任务ID过滤 | Filter by scan task ID"
// @Param status query string false "状态过滤 | Filter by status"
// @Param storage_type query string false "存储类型过滤 | Filter by storage type"
// @Param limit query int false "返回数量限制，默认20 | Result limit, default 20"
// @Param offset query int false "偏移量，默认0 | Offset, default 0"
// @Success 200 {object} map[string]interface{} "扫描运行记录列表 | Scan run list"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Security BearerAuth
func (h *MetadataHandler) ListScanRuns(c *gin.Context) {
	engineID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	authHeader, ok := extractAuthHeader(c)
	if !ok {
		return
	}

	var (
		taskID *uint
		limit  = 20
		offset = 0
	)

	if taskIDStr := c.Query("task_id"); taskIDStr != "" {
		val, err := strconv.ParseUint(taskIDStr, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
			return
		}
		taskID = new(uint)
		*taskID = uint(val)
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		val, err := strconv.Atoi(limitStr)
		if err != nil || val <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
		limit = val
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		val, err := strconv.Atoi(offsetStr)
		if err != nil || val < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset"})
			return
		}
		offset = val
	}

	status := c.Query("status")
	storageType := c.Query("storage_type")

	runs, total, err := h.metadataService.ListScanRuns(c.Request.Context(), engineID, taskID, status, storageType, limit, offset, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  runs,
		"total": total,
	})
}

// GetScanRun 获取单个运行详情
// @Summary 获取扫描运行详情 | Get scan run detail
// @Description 获取指定扫描运行记录的详细信息 | Get detailed information of a specific scan run
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param run_id path int true "扫描运行ID | Scan run ID"
// @Success 200 {object} map[string]interface{} "扫描运行详情 | Scan run detail"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Security BearerAuth
func (h *MetadataHandler) GetScanRun(c *gin.Context) {
	engineID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	runID, ok := parseUintParam(c, "run_id")
	if !ok {
		return
	}

	authHeader, ok := extractAuthHeader(c)
	if !ok {
		return
	}

	run, err := h.metadataService.GetScanRun(c.Request.Context(), engineID, runID, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": run})
}

// CreateManualScanRun 发起一次即时扫描
// @Summary 发起即时扫描 | Create manual scan run
// @Description 立即发起一次元数据扫描，不依赖定时任务 | Immediately initiate a metadata scan without relying on scheduled tasks
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param body body models.MetaManualScanRequest false "扫描配置（可选）| Scan configuration (optional)"
// @Success 200 {object} map[string]interface{} "扫描运行记录 | Scan run record"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Security BearerAuth
func (h *MetadataHandler) CreateManualScanRun(c *gin.Context) {
	engineID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	authHeader, ok := extractAuthHeader(c)
	if !ok {
		return
	}

	var req models.MetaManualScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	run, err := h.metadataService.CreateManualScanRun(c.Request.Context(), engineID, &req, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": run})
}

func parseUintParam(c *gin.Context, key string) (uint, bool) {
	value := c.Param(key)
	if strings.TrimSpace(value) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing " + key})
		return 0, false
	}

	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + key})
		return 0, false
	}

	return uint(parsed), true
}

func extractAuthHeader(c *gin.Context) (string, bool) {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if header == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return "", false
	}
	return header, true
}

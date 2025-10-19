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

// ScanResource 扫描资源元数据
// POST /api/resources/:id/scan
func (h *MetadataHandler) ScanResource(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	result, err := h.metadataService.ScanResource(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetTables 获取资源的表列表
// GET /api/resources/:id/tables?managed=true/false
func (h *MetadataHandler) GetTables(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	var isManaged *bool
	if managedStr := c.Query("managed"); managedStr != "" {
		managed := managedStr == "true"
		isManaged = &managed
	}

	tables, err := h.metadataService.GetTables(uint(id), isManaged)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  tables,
		"total": len(tables),
	})
}

// ListScanTasks 列出指定资源下的扫描任务
func (h *MetadataHandler) ListScanTasks(c *gin.Context) {
	resourceID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	authHeader, ok := extractAuthHeader(c)
	if !ok {
		return
	}

	tasks, err := h.metadataService.ListScanTasks(c.Request.Context(), resourceID, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tasks})
}

// CreateScanTask 创建新的扫描任务
func (h *MetadataHandler) CreateScanTask(c *gin.Context) {
	resourceID, ok := parseUintParam(c, "id")
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

	task, err := h.metadataService.CreateScanTask(c.Request.Context(), resourceID, &req, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": task})
}

// UpdateScanTask 更新扫描任务
func (h *MetadataHandler) UpdateScanTask(c *gin.Context) {
	resourceID, ok := parseUintParam(c, "id")
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

	task, err := h.metadataService.UpdateScanTask(c.Request.Context(), resourceID, taskID, &req, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": task})
}

// DeleteScanTask 删除扫描任务
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
func (h *MetadataHandler) ListScanRuns(c *gin.Context) {
	resourceID, ok := parseUintParam(c, "id")
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

	runs, total, err := h.metadataService.ListScanRuns(c.Request.Context(), resourceID, taskID, status, storageType, limit, offset, authHeader)
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
func (h *MetadataHandler) GetScanRun(c *gin.Context) {
	resourceID, ok := parseUintParam(c, "id")
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

	run, err := h.metadataService.GetScanRun(c.Request.Context(), resourceID, runID, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": run})
}

// CreateManualScanRun 发起一次即时扫描
func (h *MetadataHandler) CreateManualScanRun(c *gin.Context) {
	resourceID, ok := parseUintParam(c, "id")
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

	run, err := h.metadataService.CreateManualScanRun(c.Request.Context(), resourceID, &req, authHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": run})
}

// ManageTable 纳管表
// POST /api/tables/:id/manage
func (h *MetadataHandler) ManageTable(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid table id"})
		return
	}

	if err := h.metadataService.ManageTable(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "table managed successfully"})
}

// UnmanageTable 取消纳管表
// POST /api/tables/:id/unmanage
func (h *MetadataHandler) UnmanageTable(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid table id"})
		return
	}

	if err := h.metadataService.UnmanageTable(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "table unmanaged successfully"})
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

package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateUnscannedScanRuns 提交未扫描存储引擎的后台扫描运行
// @Summary 提交未扫描存储引擎后台扫描 | Submit background scans for unscanned engines
// @Description 为当前租户下尚未完成元数据扫描的存储引擎创建手动后台扫描运行 | Create manual background scan runs for engines that have not been scanned for current tenant
// @Tags Meta Scan
// @Produce json
// @Success 202 {object} map[string]interface{} "已提交的扫描运行 | Submitted scan runs"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /scan/run/unscanned [post]
// @Security BearerAuth
func (h *Handler) CreateUnscannedScanRuns(c *gin.Context) {
	if h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)
	token, ok := extractBearerToken(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}

	runs, err := h.executionService.CreateUnscannedRuns(c.Request.Context(), tenantID, userID, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"runs":      runs,
		"submitted": len(runs),
	})
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /scan/run/manual [post]
// @Security BearerAuth
func (h *Handler) CreateManualScanRun(c *gin.Context) {
	if h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	var req models.ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateManualScanRequestTriggerType(req.TriggerType); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.EngineID == 0 && req.NodeID == 0 && req.ItemID == 0 && len(req.Targets) == 0 && len(req.CatalogPaths) == 0 && len(req.RefGroups) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "engine_id, node_id, item_id, targets, catalog_paths or ref_groups is required"})
		return
	}

	token, hasBearerToken := extractBearerToken(c)
	if !hasBearerToken && strings.TrimSpace(c.GetHeader("X-Internal-API-Key")) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}

	run, err := h.executionService.CreateManualRun(c.Request.Context(), tenantID, userID, token, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, run)
}

// GetExecution 获取执行详情（按 execution UUID）
// @Summary 获取执行详情 | Get execution
// @Description 按标准 TaskProvider 路径获取执行详情 | Get execution by standard TaskProvider path
// @Tags Meta Scan
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{} "执行详情 | Execution detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "执行不存在 | Execution not found"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.read"]
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *Handler) GetExecution(c *gin.Context) {
	if h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	executionID := c.Param("execution_id")
	if executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing execution_id"})
		return
	}

	exec, err := h.executionService.GetExecution(c.Request.Context(), executionID, int(tenantID))
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.read"]
// @Router /scan/runs [get]
// @Security BearerAuth
func (h *Handler) ListScanRuns(c *gin.Context) {
	if h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution service not available"})
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

	executions, total, err := h.executionService.ListExecutions(c.Request.Context(), int(tenantID), taskID, status, triggerType, page, pageSize)
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

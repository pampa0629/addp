package api

import (
	"errors"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// CleanupHandler 资源回收任务处理器
type CleanupHandler struct {
	orchestrator *service.CleanupOrchestratorService
}

// NewCleanupHandler 创建资源回收任务处理器
func NewCleanupHandler(orchestrator *service.CleanupOrchestratorService) *CleanupHandler {
	return &CleanupHandler{
		orchestrator: orchestrator,
	}
}

func cleanupTenantID(c *gin.Context) (uint, bool) {
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		commonapi.RespondError(c, http.StatusUnauthorized, commoni18n.T(c, sysi18n.MsgCleanupTenantMissing))
		return 0, false
	}
	tenantIDValue, ok := tenantID.(uint)
	if !ok || tenantIDValue == 0 {
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgCleanupTenantRequired))
		return 0, false
	}
	return tenantIDValue, true
}

// CreateScanTaskRequest 创建扫描任务请求
type CreateScanTaskRequest struct {
	Scope []string `json:"scope"` // 评估范围：已注册且启用资源回收执行方的模块；为空则由 System 按模块注册能力生成
}

// CreateScanTaskResponse 创建扫描任务响应
type CreateScanTaskResponse struct {
	TaskID string `json:"task_id"`
}

// CreateScanTask 创建扫描任务
// @Summary 创建资源回收评估任务 | Create resource reclaim assessment task
// @Description 评估当前租户的系统级资源回收候选；scope 为空时按已注册且启用资源回收执行方的模块生成 expected_modules | Assess system resource reclaim candidates for current tenant; when scope is empty, expected_modules is generated from registered resource reclaim executors
// @Tags Resource Reclaim
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateScanTaskRequest true "扫描请求 | Scan request"
// @Success 200 {object} CreateScanTaskResponse
// @Failure 400 {string} string "错误信息 | Error message"
// @Failure 401 {string} string "错误信息 | Error message"
// @Failure 500 {string} string "错误信息 | Error message"
// @Router /admin/cleanup/scan [post]
func (h *CleanupHandler) CreateScanTask(c *gin.Context) {
	// 获取当前用户信息
	userID, exists := c.Get("user_id")
	if !exists {
		commonapi.RespondError(c, http.StatusUnauthorized, commoni18n.T(c, commoni18n.MsgUnauthorized))
		return
	}

	tenantID, ok := cleanupTenantID(c)
	if !ok {
		return
	}

	// 解析请求
	var req CreateScanTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, 400, commoni18n.TWithDetail(c, commoni18n.MsgInvalidParams, err.Error()))
		return
	}

	// 创建扫描任务
	taskID, err := h.orchestrator.CreateScanTask(
		c.Request.Context(),
		tenantID,
		req.Scope,
		userID.(uint),
	)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, commoni18n.TWithDetail(c, sysi18n.MsgCleanupCreateScanFailed, err.Error()))
		return
	}

	commonapi.RespondSuccess(c, CreateScanTaskResponse{TaskID: taskID})
}

// GetTaskStatusResponse 任务状态响应
type GetTaskStatusResponse struct {
	TaskID   string                 `json:"task_id"`
	Action   string                 `json:"action"`
	Status   string                 `json:"status"`
	Progress models.TaskProgress    `json:"progress"`
	Results  map[string]interface{} `json:"results"`
	Summary  interface{}            `json:"summary"`
	Task     models.CleanupTask     `json:"task"`
}

// GetTaskStatus 查询任务状态
// @Summary 查询资源回收任务状态 | Get resource reclaim task status
// @Description 查询指定资源回收任务的执行状态和结果 | Query execution status and results of a specified resource reclaim task
// @Tags Resource Reclaim
// @Produce json
// @Security BearerAuth
// @Param task_id path string true "任务ID | Task ID"
// @Success 200 {object} GetTaskStatusResponse
// @Failure 400 {string} string "错误信息 | Error message"
// @Failure 401 {string} string "错误信息 | Error message"
// @Failure 404 {string} string "错误信息 | Error message"
// @Failure 500 {string} string "错误信息 | Error message"
// @Router /admin/cleanup/tasks/{task_id} [get]
func (h *CleanupHandler) GetTaskStatus(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgCleanupTaskIDRequired))
		return
	}

	// 查询任务状态
	status, err := h.orchestrator.GetTaskStatus(c.Request.Context(), taskID)
	if err != nil {
		if err.Error() == "task not found" {
			commonapi.RespondError(c, http.StatusNotFound, commoni18n.T(c, sysi18n.MsgCleanupTaskNotFound))
		} else {
			commonapi.RespondError(c, http.StatusInternalServerError, commoni18n.TWithDetail(c, sysi18n.MsgCleanupGetTaskFailed, err.Error()))
		}
		return
	}

	// 权限检查：只能查看自己租户的任务
	tenantID, ok := cleanupTenantID(c)
	if !ok {
		return
	}

	if status.Task.TenantID != tenantID {
		commonapi.RespondError(c, http.StatusForbidden, commoni18n.T(c, sysi18n.MsgCleanupTaskForbidden))
		return
	}

	commonapi.RespondSuccess(c, GetTaskStatusResponse{
		TaskID:   status.TaskID,
		Action:   status.Action,
		Status:   status.Status,
		Progress: status.Progress,
		Results:  status.Results,
		Summary:  status.Summary,
		Task:     status.Task,
	})
}

// CreateExecuteTaskRequest 创建执行任务请求
type CreateExecuteTaskRequest struct {
	BasedOnScan       string `json:"based_on_scan" binding:"required"` // 基于哪次扫描
	CleanupMode       string `json:"cleanup_mode" binding:"required"`  // logical_cleanup/physical_cleanup
	Confirmed         bool   `json:"confirmed"`                        // 管理员已确认评估结果和影响范围
	ConfirmationToken string `json:"confirmation_token,omitempty"`     // 高风险或释放存储时要求输入 CONFIRM
}

// CreateExecuteTaskResponse 创建执行任务响应
type CreateExecuteTaskResponse struct {
	TaskID string `json:"task_id"`
}

// CreateExecuteTask 创建资源回收执行任务
// @Summary 创建资源回收执行任务 | Create resource reclaim execution task
// @Description 基于评估结果执行系统级资源回收；所有执行请求都必须确认，高风险或释放存储操作必须提供确认文本 CONFIRM | Execute system resource reclaim based on an assessment; every execution must be confirmed, and high-risk or storage release operations require confirmation token CONFIRM
// @Tags Resource Reclaim
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateExecuteTaskRequest true "执行请求 | Execute request"
// @Success 200 {object} CreateExecuteTaskResponse
// @Failure 400 {string} string "错误信息 | Error message"
// @Failure 401 {string} string "错误信息 | Error message"
// @Failure 500 {string} string "错误信息 | Error message"
// @Router /admin/cleanup/execute [post]
func (h *CleanupHandler) CreateExecuteTask(c *gin.Context) {
	// 获取当前用户信息
	userID, exists := c.Get("user_id")
	if !exists {
		commonapi.RespondError(c, http.StatusUnauthorized, commoni18n.T(c, commoni18n.MsgUnauthorized))
		return
	}

	// 解析请求
	var req CreateExecuteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, 400, commoni18n.TWithDetail(c, commoni18n.MsgInvalidParams, err.Error()))
		return
	}

	if err := events.ValidateCleanupMode(req.CleanupMode); err != nil {
		commonapi.RespondError(c, 400, err.Error())
		return
	}

	tenantID, ok := cleanupTenantID(c)
	if !ok {
		return
	}
	scanStatus, err := h.orchestrator.GetTaskStatus(c.Request.Context(), req.BasedOnScan)
	if err != nil {
		if err.Error() == "task not found" {
			commonapi.RespondError(c, http.StatusNotFound, commoni18n.T(c, sysi18n.MsgCleanupScanNotFound))
		} else {
			commonapi.RespondError(c, http.StatusInternalServerError, commoni18n.TWithDetail(c, sysi18n.MsgCleanupGetScanFailed, err.Error()))
		}
		return
	}
	if scanStatus.Task.TenantID != tenantID {
		commonapi.RespondError(c, http.StatusForbidden, commoni18n.T(c, sysi18n.MsgCleanupExecuteForbidden))
		return
	}
	if scanStatus.Task.Action != events.CleanupActionScan {
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgCleanupBasedOnScanRequired))
		return
	}
	if scanStatus.Status != "completed" {
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgCleanupScanNotCompleted))
		return
	}

	// 创建执行任务
	taskID, err := h.orchestrator.CreateExecuteTask(
		c.Request.Context(),
		req.BasedOnScan,
		req.CleanupMode,
		userID.(uint),
		service.CleanupExecuteConfirmation{
			Confirmed:         req.Confirmed,
			ConfirmationToken: req.ConfirmationToken,
		},
	)
	if err != nil {
		if errors.Is(err, service.ErrCleanupExecuteConfirmRequired) {
			commonapi.RespondError(c, 400, commoni18n.T(c, sysi18n.MsgCleanupConfirmRequired))
			return
		}
		if errors.Is(err, service.ErrCleanupExecuteConfirmTokenRequired) {
			commonapi.RespondError(c, 400, commoni18n.T(c, sysi18n.MsgCleanupConfirmTokenRequired))
			return
		}
		commonapi.RespondError(c, http.StatusInternalServerError, commoni18n.TWithDetail(c, sysi18n.MsgCleanupCreateExecuteFailed, err.Error()))
		return
	}

	commonapi.RespondSuccess(c, CreateExecuteTaskResponse{TaskID: taskID})
}

// GetTaskHistoryResponse 任务历史响应
type GetTaskHistoryResponse struct {
	Tasks []models.CleanupTask `json:"tasks"`
	Total int                  `json:"total"`
}

// GetTaskHistory 获取任务历史
// @Summary 获取资源回收任务历史 | Get resource reclaim task history
// @Description 获取当前租户的资源回收任务历史记录 | Get resource reclaim task history for current tenant
// @Tags Resource Reclaim
// @Produce json
// @Security BearerAuth
// @Param limit query int false "返回记录数 | Limit" default(20)
// @Success 200 {object} GetTaskHistoryResponse
// @Failure 400 {string} string "错误信息 | Error message"
// @Failure 401 {string} string "错误信息 | Error message"
// @Failure 500 {string} string "错误信息 | Error message"
// @Router /admin/cleanup/history [get]
func (h *CleanupHandler) GetTaskHistory(c *gin.Context) {
	// 获取租户信息
	tenantID, ok := cleanupTenantID(c)
	if !ok {
		return
	}

	// 获取 limit 参数
	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// 查询任务历史
	tasks, err := h.orchestrator.GetTaskHistory(
		c.Request.Context(),
		tenantID,
		limit,
	)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, commoni18n.TWithDetail(c, sysi18n.MsgCleanupGetHistoryFailed, err.Error()))
		return
	}

	commonapi.RespondSuccess(c, GetTaskHistoryResponse{
		Tasks: tasks,
		Total: len(tasks),
	})
}

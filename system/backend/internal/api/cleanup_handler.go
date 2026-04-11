package api

import (
	"strconv"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// CleanupHandler 清理任务处理器
type CleanupHandler struct {
	orchestrator *service.CleanupOrchestratorService
}

// NewCleanupHandler 创建清理任务处理器
func NewCleanupHandler(orchestrator *service.CleanupOrchestratorService) *CleanupHandler {
	return &CleanupHandler{
		orchestrator: orchestrator,
	}
}

// CreateScanTaskRequest 创建扫描任务请求
type CreateScanTaskRequest struct {
	Scope []string `json:"scope"` // 扫描范围：meta/manager/transfer 等，为空则扫描所有
}

// CreateScanTaskResponse 创建扫描任务响应
type CreateScanTaskResponse struct {
	TaskID string `json:"task_id"`
}

// CreateScanTask 创建扫描任务
// @Summary 创建垃圾数据扫描任务 | Create garbage data scan task
// @Description 扫描当前租户的垃圾数据（无效引擎、孤儿数据、软删除数据等）| Scan garbage data for current tenant (invalid engines, orphan data, soft-deleted data, etc.)
// @Tags Cleanup
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
		commonapi.RespondError(c, 401, "未授权")
		return
	}

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		commonapi.RespondError(c, 401, "租户信息缺失")
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
		tenantID.(uint),
		req.Scope,
		userID.(uint),
	)
	if err != nil {
		commonapi.RespondError(c, 500, "创建扫描任务失败: "+err.Error())
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
// @Summary 查询清理任务状态 | Get cleanup task status
// @Description 查询指定任务的执行状态和结果 | Query execution status and results of a specified task
// @Tags Cleanup
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
		commonapi.RespondError(c, 400, "任务ID不能为空")
		return
	}

	// 查询任务状态
	status, err := h.orchestrator.GetTaskStatus(c.Request.Context(), taskID)
	if err != nil {
		if err.Error() == "task not found" {
			commonapi.RespondError(c, 404, "任务不存在")
		} else {
			commonapi.RespondError(c, 500, "查询任务状态失败: "+err.Error())
		}
		return
	}

	// 权限检查：只能查看自己租户的任务
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		commonapi.RespondError(c, 401, "租户信息缺失")
		return
	}

	if status.Task.TenantID != tenantID.(uint) {
		commonapi.RespondError(c, 403, "无权访问该任务")
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
	BasedOnScan string `json:"based_on_scan" binding:"required"` // 基于哪次扫描
	DeleteType  string `json:"delete_type" binding:"required"`   // soft_delete/hard_delete
}

// CreateExecuteTaskResponse 创建执行任务响应
type CreateExecuteTaskResponse struct {
	TaskID string `json:"task_id"`
}

// CreateExecuteTask 创建执行清理任务
// @Summary 创建执行清理任务 | Create cleanup execution task
// @Description 基于扫描结果，执行垃圾数据清理（软删除或物理删除）| Execute garbage data cleanup based on scan results (soft delete or hard delete)
// @Tags Cleanup
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
		commonapi.RespondError(c, 401, "未授权")
		return
	}

	// 解析请求
	var req CreateExecuteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, 400, commoni18n.TWithDetail(c, commoni18n.MsgInvalidParams, err.Error()))
		return
	}

	// 验证 delete_type
	if req.DeleteType != "soft_delete" && req.DeleteType != "hard_delete" {
		commonapi.RespondError(c, 400, "delete_type 必须是 soft_delete 或 hard_delete")
		return
	}

	// 创建执行任务
	taskID, err := h.orchestrator.CreateExecuteTask(
		c.Request.Context(),
		req.BasedOnScan,
		req.DeleteType,
		userID.(uint),
	)
	if err != nil {
		commonapi.RespondError(c, 500, "创建执行任务失败: "+err.Error())
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
// @Summary 获取清理任务历史 | Get cleanup task history
// @Description 获取当前租户的清理任务历史记录 | Get cleanup task history for current tenant
// @Tags Cleanup
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
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		commonapi.RespondError(c, 401, "租户信息缺失")
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
		tenantID.(uint),
		limit,
	)
	if err != nil {
		commonapi.RespondError(c, 500, "查询任务历史失败: "+err.Error())
		return
	}

	commonapi.RespondSuccess(c, GetTaskHistoryResponse{
		Tasks: tasks,
		Total: len(tasks),
	})
}

package api

import (
	"context"
	"errors"
	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonauth "github.com/addp/common/middleware/auth"
	"net/http"

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
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return 0, false
	}
	return tenantID, true
}

// CreateScanTaskRequest 创建扫描任务请求
type CreateScanTaskRequest struct {
	RequestID string   `json:"request_id" binding:"required,uuid"`
	Scope     []string `json:"scope"` // 评估范围：已注册且启用资源回收执行方的模块；为空则由 System 按模块注册能力生成
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
// @Failure 403 {string} string "错误信息 | Error message"
// @Failure 409 {string} string "错误信息 | Error message"
// @Failure 503 {string} string "错误信息 | Error message"
// @Failure 500 {string} string "错误信息 | Error message"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.cleanup.execute"]
// @Router /admin/cleanup/scan [post]
func (h *CleanupHandler) CreateScanTask(c *gin.Context) {
	// 获取当前用户信息
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
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
		cleanupUserContext(c),
		req.RequestID,
		tenantID,
		req.Scope,
		actorID,
	)
	if err != nil {
		respondCleanupError(c, err)
		return
	}

	commonapi.RespondSuccess(c, CreateScanTaskResponse{TaskID: taskID})
}

// GetTaskStatusResponse 任务状态响应
type GetTaskStatusResponse = models.TaskStatusResponse

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
// @Failure 403 {string} string "错误信息 | Error message"
// @Failure 409 {string} string "错误信息 | Error message"
// @Failure 503 {string} string "错误信息 | Error message"
// @Failure 404 {string} string "错误信息 | Error message"
// @Failure 500 {string} string "错误信息 | Error message"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.cleanup.read"]
// @Router /admin/cleanup/tasks/{task_id} [get]
func (h *CleanupHandler) GetTaskStatus(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgCleanupTaskIDRequired))
		return
	}

	reader, err := cleanupReader(c)
	if err != nil {
		respondCleanupError(c, err)
		return
	}
	status, err := h.orchestrator.GetTaskStatus(c.Request.Context(), taskID, reader)
	if err != nil {
		respondCleanupError(c, err)
		return
	}
	commonapi.RespondSuccess(c, status)
}

// CreateExecuteTaskRequest 创建执行任务请求
type CreateExecuteTaskRequest struct {
	RequestID              string `json:"request_id" binding:"required,uuid"`
	BasedOnScan            string `json:"based_on_scan" binding:"required"`                          // 基于哪次扫描
	CleanupMode            string `json:"cleanup_mode" binding:"required"`                           // logical_cleanup/physical_cleanup
	Confirmed              bool   `json:"confirmed"`                                                 // 管理员已确认评估结果和影响范围
	ConfirmationToken      string `json:"confirmation_token,omitempty"`                              // 高风险或释放存储时要求输入 CONFIRM
	ExternalArtifactPolicy string `json:"external_artifact_policy,omitempty" enums:"delete,abandon"` // 物理回收必须显式选择：delete/abandon
}

// CreateExecuteTaskResponse 创建执行任务响应
type CreateExecuteTaskResponse struct {
	TaskID string `json:"task_id"`
}

// CreateExecuteTask 创建资源回收执行任务
// @Summary 创建资源回收执行任务 | Create resource reclaim execution task
// @Description 基于评估结果执行系统级资源回收；所有执行请求都必须确认，高风险或释放存储操作必须提供确认文本 CONFIRM，物理回收必须显式选择外部产物策略 delete 或 abandon | Execute system resource reclaim based on an assessment; every execution must be confirmed, high-risk or storage release operations require confirmation token CONFIRM, and physical reclaim requires an explicit external artifact policy of delete or abandon
// @Tags Resource Reclaim
// @Accept json
// @Produce json
// @Security BearerAuth
// @Description Meta清理须在同一次评估中显式包含Manager，否则409，范围不自动扩展。 | Meta cleanup requires Manager in the same assessment; otherwise 409, without automatic scope expansion.
// @Param request body CreateExecuteTaskRequest true "执行请求 | Execute request"
// @Success 200 {object} CreateExecuteTaskResponse
// @Failure 400 {string} string "错误信息 | Error message"
// @Failure 401 {string} string "错误信息 | Error message"
// @Failure 403 {string} string "错误信息 | Error message"
// @Failure 409 {string} string "错误信息 | Error message"
// @Failure 503 {string} string "错误信息 | Error message"
// @Failure 500 {string} string "错误信息 | Error message"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.cleanup.execute"]
// @Router /admin/cleanup/execute [post]
func (h *CleanupHandler) CreateExecuteTask(c *gin.Context) {
	// 获取当前用户信息
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
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

	// 创建执行任务
	taskID, err := h.orchestrator.CreateExecuteTask(
		cleanupUserContext(c),
		req.RequestID,
		req.BasedOnScan,
		req.CleanupMode,
		tenantID,
		actorID,
		service.CleanupExecuteConfirmation{
			Confirmed:              req.Confirmed,
			ConfirmationToken:      req.ConfirmationToken,
			ExternalArtifactPolicy: req.ExternalArtifactPolicy,
		},
	)
	if err != nil {
		if errors.Is(err, service.ErrCleanupManagerScopeRequired) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error_code": "cleanup_manager_scope_required", "error": commoni18n.T(c, sysi18n.MsgCleanupManagerScopeRequired)})
			return
		}
		if errors.Is(err, service.ErrCleanupExecuteConfirmRequired) {
			commonapi.RespondError(c, 400, commoni18n.T(c, sysi18n.MsgCleanupConfirmRequired))
			return
		}
		if errors.Is(err, service.ErrCleanupExecuteConfirmTokenRequired) {
			commonapi.RespondError(c, 400, commoni18n.T(c, sysi18n.MsgCleanupConfirmTokenRequired))
			return
		}
		if errors.Is(err, service.ErrCleanupExternalArtifactPolicyRequired) {
			commonapi.RespondError(c, 400, commoni18n.T(c, sysi18n.MsgCleanupArtifactPolicyRequired))
			return
		}
		if errors.Is(err, service.ErrCleanupExternalArtifactPolicyInvalid) || errors.Is(err, service.ErrCleanupExternalArtifactPolicyNotAllowed) {
			commonapi.RespondError(c, 400, commoni18n.T(c, sysi18n.MsgCleanupArtifactPolicyInvalid))
			return
		}
		respondCleanupError(c, err)
		return
	}

	commonapi.RespondSuccess(c, CreateExecuteTaskResponse{TaskID: taskID})
}

// GetTaskHistoryResponse 任务历史响应
type GetTaskHistoryResponse struct {
	Tasks    []models.CleanupTask `json:"tasks"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

// GetTaskHistory 获取任务历史
// @Summary 获取资源回收任务历史 | Get resource reclaim task history
// @Description 获取当前租户的资源回收任务历史记录 | Get resource reclaim task history for current tenant
// @Tags Resource Reclaim
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页记录数 | Page size" default(20)
// @Success 200 {object} GetTaskHistoryResponse
// @Failure 400 {string} string "错误信息 | Error message"
// @Failure 401 {string} string "错误信息 | Error message"
// @Failure 403 {string} string "错误信息 | Error message"
// @Failure 409 {string} string "错误信息 | Error message"
// @Failure 503 {string} string "错误信息 | Error message"
// @Failure 500 {string} string "错误信息 | Error message"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.cleanup.read"]
// @Router /admin/cleanup/history [get]
func (h *CleanupHandler) GetTaskHistory(c *gin.Context) {
	actor, tenant, err := iamTenantUserActor(c)
	if err != nil {
		respondCleanupError(c, err)
		return
	}
	page, size := commonapi.ParsePagination(c)
	tasks, total, err := h.orchestrator.GetTaskHistory(c.Request.Context(), tenant, actor, page, size)
	if err != nil {
		respondCleanupError(c, err)
		return
	}
	commonapi.RespondSuccess(c, GetTaskHistoryResponse{Tasks: tasks, Total: total, Page: page, PageSize: size})
}

func cleanupUserContext(c *gin.Context) context.Context {
	facts, _ := commonauth.AuthContextFromGin(c)
	return execution.WithUserOrigin(c.Request.Context(), facts)
}
func cleanupReader(c *gin.Context) (service.CleanupReader, error) {
	actor, tenant, err := iamTenantUserActor(c)
	if err != nil {
		return service.CleanupReader{}, err
	}
	facts, ok := commonauth.AuthContextFromGin(c)
	if !ok {
		return service.CleanupReader{}, commonapi.ErrUnauthorized
	}
	return service.CleanupReader{TenantID: tenant, UserID: actor, OwnerProtocol: facts.Token.Type == "first_party_access_token" && facts.Client.ScopeMode == "unrestricted", Bearer: c.GetHeader("Authorization")}, nil
}
func respondCleanupError(c *gin.Context, err error) {
	if errors.Is(err, client.ErrExecutionOwnerUnavailable) {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error_code": "execution_owner_unavailable", "error": commoni18n.T(c, commoni18n.MsgAuthorizationServiceUnavailable)})
		return
	}
	respondIAMError(c, err)
}

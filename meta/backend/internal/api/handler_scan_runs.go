package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/taskprovider"
	metai18n "github.com/addp/meta/i18n"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/gin-gonic/gin"
)

// CreateUnscannedScanRuns 提交未扫描存储引擎的后台扫描运行
// @Summary 提交未扫描存储引擎后台扫描 | Submit background scans for unscanned engines
// @Description 为当前租户下尚未完成元数据扫描的存储引擎创建手动后台扫描运行；单个引擎提交失败不会中断后续引擎，响应包含 submission_failures | Create manual background scan runs for unscanned tenant engines; per-engine submission failures do not stop later engines and are returned in submission_failures
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
	result, err := h.executionService.CreateUnscannedRuns(c.Request.Context(), tenantID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"runs":                models.NewScanExecutionResponses(result.Runs),
		"submitted":           len(result.Runs),
		"submission_failures": result.SubmissionFailures,
	})
}

// CreateManualScanRun 创建异步扫描运行
// @Summary 创建手动扫描运行 | Create manual scan run
// @Description 叶子 locator 仅扫描自身，root／branch 范围必须显式指定；创建异步扫描；Develop 产物来源仅允许 addp-develop 服务携带父 execution，原子校验已保存产物并继承发起主体 | Leaf locators scan only the selected item; root and branch scopes must be explicit. Create an async scan; Develop output scans require addp-develop service provenance, a verified parent execution and persisted output
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param request body models.ScanRequest true "扫描请求 | Scan request"
// @Success 201 {object} models.ScanExecutionResponse "安全执行记录 | Safe execution"
// @Failure 403 {object} map[string]interface{} "不允许使用父执行来源 | Parent provenance is not permitted"
// @Failure 404 {object} map[string]interface{} "父执行或产物来源不可用 | Parent or output provenance unavailable"
// @Failure 409 {object} map[string]interface{} "同范围扫描正在执行：scan_scope_active | Scan scope already active: scan_scope_active"
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
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error_code": "scan_request_invalid", "error": commoni18n.T(c, metai18n.MsgScanRequestInvalid)})
		return
	}
	req.Source = strings.TrimSpace(req.Source)
	if err := validateManualScanRequestTriggerType(req.TriggerType); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.EngineID == 0 && req.NodeID == 0 && req.ItemID == 0 && len(req.Targets) == 0 && len(req.CatalogPaths) == 0 && len(req.RefGroups) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "engine_id, node_id, item_id, targets, catalog_paths or ref_groups is required"})
		return
	}

	if req.ParentExecutionID != "" || req.Source == commonclient.MetaScanSourceDevelopProducedTarget {
		principal, exists := commonAuth.PrincipalFromGin(c)
		if !exists || principal.Type != "service_principal" || commonAuth.GetClientID(c) != "addp-develop" {
			c.JSON(http.StatusForbidden, gin.H{"error_code": "scan_provenance_denied", "error": commoni18n.T(c, metai18n.MsgScanProvenanceDenied)})
			return
		}
	}
	run, err := h.executionService.CreateManualRun(c.Request.Context(), tenantID, userID, &req)
	if err != nil {
		if errors.Is(err, scanflow.ErrInvalidScope) {
			c.JSON(http.StatusBadRequest, gin.H{"error_code": "scan_request_invalid", "error": commoni18n.T(c, metai18n.MsgScanRequestInvalid)})
			return
		}
		if errors.Is(err, commonapi.ErrBadRequest) || errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(commonapi.MapErrorToHTTPStatus(err), gin.H{"error_code": "scan_provenance_unavailable", "error": commoni18n.T(c, metai18n.MsgScanProvenanceUnavailable)})
			return
		}
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, models.NewScanExecutionResponse(run))
}

// GetExecution 获取执行详情（按 execution UUID）
// @Summary 获取执行详情 | Get execution
// @Description 当前租户 meta/scan 的安全扫描详情；任务历史按读取权限，一次性执行仅发起 User 可读 | Safe tenant meta/scan observation; permission covers task history, ad-hoc executions are initiator-only
// @Tags Meta Scan
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} models.ScanExecutionResponse "安全执行详情 | Safe execution detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "用户身份或权限不允许 | User identity or permission denied"
// @Failure 404 {object} map[string]interface{} "执行不存在或不可读 | Execution absent or invisible"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.read"]
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *Handler) GetExecution(c *gin.Context) {
	if !installScanUserReadScope(c) {
		return
	}
	exec, ok := h.loadExecution(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, models.NewScanExecutionResponse(exec))
}

// ProviderGetExecution 获取 TaskProvider 执行状态。
// @Summary 获取 TaskProvider 扫描执行状态 | Get TaskProvider scan execution status
// @Tags Meta Scan
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} taskprovider.ExecutionStatusResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.task_provider.read"]
// @Router /task-provider/executions/{execution_id} [get]
// @Security BearerAuth
func (h *Handler) ProviderGetExecution(c *gin.Context) {
	exec, ok := h.loadExecution(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, taskprovider.NewExecutionStatusResponse(exec))
}

func (h *Handler) loadExecution(c *gin.Context) (*commonExecution.TaskExecution, bool) {
	if h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution service not available"})
		return nil, false
	}

	tenantID := commonAuth.GetTenantID(c)
	executionID := c.Param("execution_id")
	if executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing execution_id"})
		return nil, false
	}

	exec, err := h.executionService.GetExecution(c.Request.Context(), executionID, int(tenantID))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error_code": "execution_not_found", "error": commoni18n.T(c, metai18n.MsgScanExecutionNotFound)})
			return nil, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error_code": "execution_read_failed", "error": commoni18n.T(c, metai18n.MsgScanExecutionReadFailed)})
		return nil, false
	}
	return exec, true
}

// ListScanRuns 列出执行记录（从 common.task_executions 查询）
// @Summary 列出扫描运行 | List scan runs
// @Description 当前租户 meta/scan 的安全运行记录；列表和总数仅包含可读任务历史及本人一次性执行 | Safe meta/scan executions; list and total contain readable task history and own ad-hoc executions only
// @Tags Meta Scan
// @Produce json
// @Param task_id query int false "任务ID | Task ID"
// @Param status query string false "执行状态 | Execution status"
// @Param trigger_type query string false "触发类型 | Trigger type"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} models.ScanExecutionListResponse "安全分页执行记录 | Safe paged executions"
// @Failure 403 {object} map[string]interface{} "用户身份或权限不允许 | User identity or permission denied"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.read"]
// @Router /scan/runs [get]
// @Security BearerAuth
func (h *Handler) ListScanRuns(c *gin.Context) {
	if !installScanUserReadScope(c) {
		return
	}
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
		c.JSON(http.StatusInternalServerError, gin.H{"error_code": "execution_read_failed", "error": commoni18n.T(c, metai18n.MsgScanExecutionReadFailed)})
		return
	}

	totalPages := int64(0)
	if pageSize > 0 {
		totalPages = (total + int64(pageSize) - 1) / int64(pageSize)
	}

	c.JSON(http.StatusOK, models.ScanExecutionListResponse{
		Items: models.NewScanExecutionResponses(executions), Total: total, Page: page, PageSize: pageSize, TotalPages: totalPages,
	})
}

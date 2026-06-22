package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	_ "github.com/addp/orchestrator/i18n"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
)

// OrchestrationHandler 编排 API 处理器
type OrchestrationHandler struct {
	orchRepo             *repository.OrchestrationRepository
	executionService     *service.ExecutionService
	executor             *service.Executor
	taskProviderRegistry *service.TaskProviderRegistry
	httpClient           *http.Client
}

type orchestrationTaskProviderExecuteRequest struct {
	TriggerType       string                 `json:"trigger_type"`
	Source            string                 `json:"source"`
	ParentExecutionID string                 `json:"parent_execution_id"`
	Parameters        map[string]interface{} `json:"parameters"`
}

type orchestrationTaskProviderExecuteResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
}

// NewOrchestrationHandler 创建处理器
func NewOrchestrationHandler(
	orchRepo *repository.OrchestrationRepository,
	executionService *service.ExecutionService,
	executor *service.Executor,
	taskProviderRegistry *service.TaskProviderRegistry,
	httpClient *http.Client,
) *OrchestrationHandler {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &OrchestrationHandler{
		orchRepo:             orchRepo,
		executionService:     executionService,
		executor:             executor,
		taskProviderRegistry: taskProviderRegistry,
		httpClient:           httpClient,
	}
}

// Create 创建编排
// @Summary 创建编排 | Create orchestration
// @Tags Orchestrator
// @Accept json
// @Produce json
// @Param orchestration body models.Orchestration true "编排定义，steps 必须使用 provider/task_type/task_id 任务引用 | Orchestration definition, steps must use provider/task_type/task_id task references"
// @Success 201 {object} map[string]interface{}
// @Router /orchestrations [post]
// @Security BearerAuth
func (h *OrchestrationHandler) Create(c *gin.Context) {
	var req models.Orchestration
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: 从 JWT 中提取 tenant_id
	req.ID = 0
	req.TenantID = 1

	if err := models.ValidateSteps(req.Steps); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.taskProviderRegistry.ValidateStepTaskReferences(c.Request.Context(), req.Steps); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.ValidateNoRecursiveOrchestrationReferences(h.orchRepo, req.ID, req.TenantID, req.Steps); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.ApplyOrchestrationSchedule(&req, time.Now()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.orchRepo.Create(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, req)
}

// List 列出编排
// @Summary 获取编排列表 | List orchestrations
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations [get]
// @Security BearerAuth
func (h *OrchestrationHandler) List(c *gin.Context) {
	// TODO: 从 JWT 中提取 tenant_id
	tenantID := uint(1)

	orchs, err := h.orchRepo.List(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, orchs)
}

// Get 获取编排详情
// @Summary 获取编排详情 | Get orchestration detail
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations/{id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	orch, err := h.orchRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}

	c.JSON(http.StatusOK, orch)
}

// Update 更新编排
// @Summary 更新编排 | Update orchestration
// @Tags Orchestrator
// @Accept json
// @Produce json
// @Param id path int true "编排 ID | Orchestration ID"
// @Param orchestration body models.Orchestration true "编排定义，steps 必须使用 provider/task_type/task_id 任务引用 | Orchestration definition, steps must use provider/task_type/task_id task references"
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations/{id} [put]
// @Security BearerAuth
func (h *OrchestrationHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	orch, err := h.orchRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}

	var req models.Orchestration
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 保留原有字段
	req.ID = orch.ID
	req.TenantID = orch.TenantID

	if err := models.ValidateSteps(req.Steps); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.taskProviderRegistry.ValidateStepTaskReferences(c.Request.Context(), req.Steps); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.ValidateNoRecursiveOrchestrationReferences(h.orchRepo, req.ID, req.TenantID, req.Steps); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.ApplyOrchestrationSchedule(&req, time.Now()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.orchRepo.Update(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, req)
}

// Delete 删除编排
// @Summary 删除编排 | Delete orchestration
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations/{id} [delete]
// @Security BearerAuth
func (h *OrchestrationHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	if err := h.orchRepo.Delete(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, "orchestrator.success.deleted")})
}

// Execute 手动触发编排执行
// @Summary 执行编排 | Execute orchestration
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations/{id}/execute [post]
// @Security BearerAuth
func (h *OrchestrationHandler) Execute(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	orch, err := h.orchRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}

	// 使用统一执行服务创建执行记录
	ctx := c.Request.Context()
	execution, err := h.executionService.CreateExecution(ctx, orch.ID, orch.TenantID, commonExecution.TriggerTypeManual)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.executor.ExecuteAsync(uint(execution.ID))

	c.JSON(http.StatusAccepted, gin.H{
		"id":           execution.ID,
		"execution_id": execution.ExecutionID,
		"status":       execution.Status,
	})
}

// ListExecutions 列出编排的执行记录
// @Summary 获取执行列表 | List executions
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations/{id}/executions [get]
// @Security BearerAuth
func (h *OrchestrationHandler) ListExecutions(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	// TODO: 从 JWT 中提取 tenant_id
	tenantID := uint(1)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	ctx := c.Request.Context()
	execs, total, err := h.executionService.ListExecutions(ctx, uint(id), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		totalPages++
	}
	c.JSON(http.StatusOK, gin.H{
		"data":        execs,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// ListAllExecutions 列出所有执行记录
// @Summary 获取所有执行列表 | List all executions
// @Tags Orchestrator
// @Produce json
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} map[string]interface{}
// @Router /executions [get]
// @Security BearerAuth
func (h *OrchestrationHandler) ListAllExecutions(c *gin.Context) {
	// TODO: 从 JWT 中提取 tenant_id
	tenantID := uint(1)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	ctx := c.Request.Context()
	execs, total, err := h.executionService.ListAllExecutions(ctx, tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	totalPages2 := int(total) / pageSize
	if int(total)%pageSize != 0 {
		totalPages2++
	}
	c.JSON(http.StatusOK, gin.H{
		"data":        execs,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages2,
	})
}

// GetExecution 获取执行详情
// @Summary 获取执行详情 | Get execution detail
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orch-executions/{id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) GetExecution(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	// TODO: 从 JWT 中提取 tenant_id
	tenantID := uint(1)

	ctx := c.Request.Context()
	exec, err := h.executionService.GetExecution(ctx, uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.execution_not_found")})
		return
	}

	c.JSON(http.StatusOK, exec)
}

// ListTaskProviders 列出所有任务提供者（从 System 的 task_providers 表获取）
// @Summary 列出任务提供者 | List task providers
// @Description 从 System 任务提供者注册表获取可编排任务提供者 | List task providers from System task provider registry
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /task-providers [get]
// @Security BearerAuth
func (h *OrchestrationHandler) ListTaskProviders(c *gin.Context) {
	ctx := c.Request.Context()

	// 使用 TaskProviderRegistry 获取所有任务提供者
	providers, err := h.taskProviderRegistry.ListAllProviders(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, "orchestrator.error.list_providers_failed", err.Error())})
		return
	}

	c.JSON(http.StatusOK, providers)
}

// ListModuleTasks 列出指定模块的任务（动态调用）
// @Summary 列出模块任务 | List module tasks
// @Description 动态调用目标模块任务列表接口并标准化返回格式；module_name=orchestrator 时返回本模块已保存的编排任务。| Proxy and normalize module task list; when module_name=orchestrator, return saved orchestration tasks.
// @Tags Orchestrator
// @Produce json
// @Param module_name query string false "模块名；为空或 orchestrator 时返回编排任务 | Module name; empty or orchestrator returns orchestration tasks"
// @Param task_type query string false "任务类型；Orchestrator 固定为 orchestration | Task type; orchestration for Orchestrator"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(100)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Failure 502 {object} map[string]interface{}
// @Router /tasks [get]
// @Security BearerAuth
func (h *OrchestrationHandler) ListModuleTasks(c *gin.Context) {
	moduleName := strings.TrimSpace(c.Query("module_name"))
	if moduleName == "" || moduleName == commonExecution.ModuleOrchestrator {
		h.ListProviderOrchestrationTasks(c)
		return
	}

	ctx := c.Request.Context()

	// 1. 从 TaskProviderRegistry 获取任务提供者配置
	provider, err := h.taskProviderRegistry.GetProvider(ctx, moduleName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.TWithDetail(c, "orchestrator.error.provider_not_found", fmt.Sprintf("%s: %v", moduleName, err))})
		return
	}
	if err := validateRequestedProviderTaskType(provider, c.Query("task_type")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 2. 构建目标 URL
	if provider.BaseURL == "" || provider.TaskListEndpoint == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, "orchestrator.error.provider_not_configured")})
		return
	}

	targetURL := provider.BaseURL + provider.TaskListEndpoint

	// 3. 传递请求中的查询参数（page, page_size, task_type 等）
	queryParams := url.Values{}
	for key, values := range c.Request.URL.Query() {
		if key != "module_name" && len(values) > 0 {
			queryParams.Set(key, values[0])
		}
	}

	// 设置默认分页参数（如果不存在）
	if !queryParams.Has("page") {
		queryParams.Set("page", "1")
	}
	if !queryParams.Has("page_size") {
		queryParams.Set("page_size", "100")
	}

	// 构建完整 URL
	if len(queryParams) > 0 {
		targetURL += "?" + queryParams.Encode()
	}

	// 4. 发送 HTTP 请求（默认使用 GET 方法）
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, "orchestrator.error.create_request_failed", err.Error())})
		return
	}

	// 传递 Authorization 头（如果存在）
	if authHeader := c.GetHeader("Authorization"); authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": commoni18n.TWithDetail(c, "orchestrator.error.call_module_api_failed", err.Error())})
		return
	}
	defer resp.Body.Close()

	// 6. 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, "orchestrator.error.read_response_failed", err.Error())})
		return
	}

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":       fmt.Sprintf("%s，状态码: %d", commoni18n.T(c, "orchestrator.error.module_api_error"), resp.StatusCode),
			"target_url":  targetURL,
			"status_code": resp.StatusCode,
			"body":        string(body),
		})
		return
	}

	// 7. 解析并标准化响应格式
	var respData interface{}
	if err := json.Unmarshal(body, &respData); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":      commoni18n.TWithDetail(c, "orchestrator.error.parse_response_failed", err.Error()),
			"target_url": targetURL,
			"body":       string(body),
		})
		return
	}

	// 8. 统一返回格式
	result, err := h.standardizeTaskListResponse(respData)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":      commoni18n.TWithDetail(c, "orchestrator.error.parse_response_failed", err.Error()),
			"target_url": targetURL,
			"body":       string(body),
		})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListProviderOrchestrationTasks 列出 Orchestrator 自身可编排任务。
func (h *OrchestrationHandler) ListProviderOrchestrationTasks(c *gin.Context) {
	taskType := strings.TrimSpace(c.Query("task_type"))
	if taskType != "" && taskType != commonExecution.TaskTypeOrchestration {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	if tenantID == 0 {
		tenantID = 1
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}

	orchs, total, err := h.orchRepo.ListPaged(tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]models.ProviderOrchestrationTask, 0, len(orchs))
	for _, orch := range orchs {
		items = append(items, models.NewProviderOrchestrationTask(orch))
	}

	c.JSON(http.StatusOK, models.ListProviderOrchestrationTasksResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// GetProviderOrchestrationTask 获取 Orchestrator 编排任务详情。
// @Summary 获取 TaskProvider 编排任务详情 | Get TaskProvider orchestration task detail
// @Description 按标准 TaskProvider 路径获取 Orchestrator 编排任务详情；task_type 仅支持 orchestration。| Get Orchestrator task detail through the standard TaskProvider path; task_type only supports orchestration.
// @Tags Orchestrator
// @Produce json
// @Param task_type path string true "任务类型，固定为 orchestration | Task type, fixed to orchestration"
// @Param id path int true "编排 ID | Orchestration ID"
// @Success 200 {object} models.ProviderOrchestrationTask
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /tasks/{task_type}/{id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) GetProviderOrchestrationTask(c *gin.Context) {
	if c.Param("task_type") != commonExecution.TaskTypeOrchestration {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + c.Param("task_type")})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	if tenantID == 0 {
		tenantID = 1
	}
	orch, err := h.orchRepo.GetByIDAndTenant(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}
	c.JSON(http.StatusOK, models.NewProviderOrchestrationTask(*orch))
}

// ExecuteProviderOrchestrationTask 执行 Orchestrator 编排任务。
// @Summary 执行 TaskProvider 编排任务 | Execute TaskProvider orchestration task
// @Description 按标准 TaskProvider 协议执行 Orchestrator 编排任务；task_type 仅支持 orchestration。| Execute an Orchestrator task through the standard TaskProvider protocol; task_type only supports orchestration.
// @Tags Orchestrator
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型，固定为 orchestration | Task type, fixed to orchestration"
// @Param id path int true "编排 ID | Orchestration ID"
// @Param request body orchestrationTaskProviderExecuteRequest false "TaskProvider 执行请求 | TaskProvider execution request"
// @Success 202 {object} orchestrationTaskProviderExecuteResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *OrchestrationHandler) ExecuteProviderOrchestrationTask(c *gin.Context) {
	if c.Param("task_type") != commonExecution.TaskTypeOrchestration {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + c.Param("task_type")})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	var req orchestrationTaskProviderExecuteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Parameters) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Orchestrator task provider does not support execution parameter overrides"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	if tenantID == 0 {
		tenantID = 1
	}
	if _, err := h.orchRepo.GetByIDAndTenant(uint(id), tenantID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = commonExecution.ModuleOrchestrator
	}
	var parentExecutionID *string
	if strings.TrimSpace(req.ParentExecutionID) != "" {
		parentExecutionID = &req.ParentExecutionID
	}

	execution, err := h.executionService.CreateExecutionWithContext(
		c.Request.Context(),
		uint(id),
		tenantID,
		req.TriggerType,
		source,
		parentExecutionID,
		commonAuth.GetUserID(c),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.executor.ExecuteAsync(uint(execution.ID))

	c.JSON(http.StatusAccepted, orchestrationTaskProviderExecuteResponse{
		ExecutionID: execution.ExecutionID,
		Status:      execution.Status,
	})
}

// GetProviderExecution 获取 Orchestrator 编排执行状态。
// @Summary 获取 Orchestrator 编排执行状态 | Get Orchestrator execution status
// @Description 按 execution_id 查询 Orchestrator 编排统一执行记录。| Get an Orchestrator execution record by execution_id.
// @Tags Orchestrator
// @Produce json
// @Param execution_id path string true "执行 UUID | Execution UUID"
// @Success 200 {object} commonExecution.TaskExecution
// @Failure 404 {object} map[string]interface{}
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) GetProviderExecution(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	if tenantID == 0 {
		tenantID = 1
	}
	exec, err := h.executionService.GetExecutionByExecutionID(c.Request.Context(), c.Param("execution_id"), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.execution_not_found")})
		return
	}
	c.JSON(http.StatusOK, exec)
}

// standardizeTaskListResponse 统一任务列表响应格式。
// TaskProvider 列表契约使用 {items,total,page,page_size}；历史模块使用的非标准格式不再兼容。
func (h *OrchestrationHandler) standardizeTaskListResponse(respData interface{}) (map[string]interface{}, error) {
	resultMap, ok := respData.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("TaskProvider task list response must be an object with items")
	}

	parsed, err := taskprovider.ParseTaskListObject(resultMap)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"items":     h.ensureDisplayNames(parsed.Items),
		"total":     parsed.Total,
		"page":      parsed.Page,
		"page_size": parsed.PageSize,
	}, nil
}

func validateRequestedProviderTaskType(provider *commonModels.TaskProvider, taskType string) error {
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return nil
	}
	if provider == nil {
		return fmt.Errorf("provider is nil")
	}
	if provider.Capabilities == nil || strings.TrimSpace(string(*provider.Capabilities)) == "" {
		return fmt.Errorf("provider %q capabilities is required", provider.ModuleName)
	}
	capabilities, err := taskprovider.ParseCapabilities(string(*provider.Capabilities))
	if err != nil {
		return fmt.Errorf("provider %q capabilities invalid: %w", provider.ModuleName, err)
	}
	capability := capabilities.CapabilityFor(taskType)
	if capability == nil {
		return fmt.Errorf("task_type %q is not declared by provider %q", taskType, provider.ModuleName)
	}
	if capability.Deprecated {
		return fmt.Errorf("task_type %q of provider %q is deprecated", taskType, provider.ModuleName)
	}
	return nil
}

// ensureDisplayNames 确保任务列表中每个任务都有 display_name 字段
// 如果没有 display_name，则使用 name 作为默认值
func (h *OrchestrationHandler) ensureDisplayNames(items interface{}) []interface{} {
	itemsArray, ok := items.([]interface{})
	if !ok {
		return []interface{}{}
	}

	result := make([]interface{}, len(itemsArray))
	for i, item := range itemsArray {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			result[i] = item
			continue
		}

		// 如果没有 display_name 字段，使用 name 作为默认值
		if _, hasDisplayName := itemMap["display_name"]; !hasDisplayName {
			if name, hasName := itemMap["name"]; hasName {
				itemMap["display_name"] = name
			}
		}

		result[i] = itemMap
	}

	return result
}

// replaceTemplateVars 替换模板变量（如 {{.TenantID}}）
// 支持的变量：
// - {{.TenantID}} - 从请求中获取租户 ID（优先从 query，其次从 JWT）
// - {{.UserID}} - 从 JWT 中获取用户 ID
// - {{.Page}} - 从请求中获取page参数
// - {{.PageSize}} - 从请求中获取page_size参数
func replaceTemplateVars(template string, c *gin.Context) string {
	result := template

	// 替换 {{.TenantID}}
	if strings.Contains(result, "{{.TenantID}}") {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			// TODO: 从 JWT 中提取 tenant_id
			tenantID = "1" // 默认值
		}
		result = strings.ReplaceAll(result, "{{.TenantID}}", tenantID)
	}

	// 替换 {{.UserID}}
	if strings.Contains(result, "{{.UserID}}") {
		// TODO: 从 JWT 中提取 user_id
		userID := "1" // 默认值
		result = strings.ReplaceAll(result, "{{.UserID}}", userID)
	}

	// 替换 {{.Page}}
	if strings.Contains(result, "{{.Page}}") {
		page := c.DefaultQuery("page", "1")
		result = strings.ReplaceAll(result, "{{.Page}}", page)
	}

	// 替换 {{.PageSize}}
	if strings.Contains(result, "{{.PageSize}}") {
		pageSize := c.DefaultQuery("page_size", "20")
		result = strings.ReplaceAll(result, "{{.PageSize}}", pageSize)
	}

	return result
}

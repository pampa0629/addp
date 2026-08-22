package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// OrchestrationHandler 编排 API 处理器
type OrchestrationHandler struct {
	orchRepo                *repository.OrchestrationRepository
	executionService        *service.ExecutionService
	executor                *service.Executor
	taskProviderResolver    *service.TaskProviderResolver
	httpClient              *http.Client
	taskAuthorizationClient *commonClient.SystemExecutionAuthorizationClient
	serviceTokens           commonClient.ServiceTokenProvider
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
	taskProviderResolver *service.TaskProviderResolver,
	httpClient *http.Client,
	taskAuthorizationClient *commonClient.SystemExecutionAuthorizationClient,
	serviceTokens commonClient.ServiceTokenProvider,
) *OrchestrationHandler {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &OrchestrationHandler{
		orchRepo:                orchRepo,
		executionService:        executionService,
		executor:                executor,
		taskProviderResolver:    taskProviderResolver,
		httpClient:              httpClient,
		taskAuthorizationClient: taskAuthorizationClient,
		serviceTokens:           serviceTokens,
	}
}

// Create 创建编排
// @Summary 创建编排 | Create orchestration
// @Description 严格解析编排定义并拒绝未知字段，统一校验 Step、DAG、模板依赖、任务引用、递归引用和调度表达式 | Strictly parse the orchestration definition, reject unknown fields, and validate steps, DAG, template dependencies, task references, recursive references, and schedule expressions
// @Tags Orchestrator
// @Accept json
// @Produce json
// @Param orchestration body models.OrchestrationDefinitionRequest true "编排定义，steps 必须使用 provider/task_type/task_id 任务引用 | Orchestration definition, steps must use provider/task_type/task_id task references"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} models.ErrorResponse "编排定义或执行参数无效 | Invalid orchestration definition or execution parameters"
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.create"]
// @Router /orchestrations [post]
// @Security BearerAuth
func (h *OrchestrationHandler) Create(c *gin.Context) {
	var input models.OrchestrationDefinitionRequest
	if !bindOrchestrationRequest(c, &input) {
		return
	}
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	orch := models.Orchestration{TenantID: tenantID}
	input.ApplyTo(&orch)
	if userID := commonAuth.GetUserID(c); userID > 0 {
		orch.CreatedBy = &userID
	}

	if !h.validateOrchestrationDefinition(c, &orch) {
		return
	}
	if !h.authorizeOrchestrationDefinition(c, &orch, input) {
		return
	}

	if err := h.orchRepo.Create(&orch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, orch)
}

// List 列出编排
// @Summary 获取编排列表 | List orchestrations
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
// @Router /orchestrations [get]
// @Security BearerAuth
func (h *OrchestrationHandler) List(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

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
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @Failure 404 {object} models.ErrorResponse "编排不存在 | Orchestration not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
// @Router /orchestrations/{id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	orch, err := h.orchRepo.GetByIDAndTenant(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}

	c.JSON(http.StatusOK, orch)
}

// Update 更新编排
// @Summary 更新编排 | Update orchestration
// @Description 严格解析编排定义并拒绝未知字段，统一校验 Step、DAG、模板依赖、任务引用、递归引用和调度表达式 | Strictly parse the orchestration definition, reject unknown fields, and validate steps, DAG, template dependencies, task references, recursive references, and schedule expressions
// @Tags Orchestrator
// @Accept json
// @Produce json
// @Param id path int true "编排 ID | Orchestration ID"
// @Param orchestration body models.OrchestrationDefinitionRequest true "编排定义，steps 必须使用 provider/task_type/task_id 任务引用 | Orchestration definition, steps must use provider/task_type/task_id task references"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} models.ErrorResponse "编排定义或执行参数无效 | Invalid orchestration definition or execution parameters"
// @Failure 404 {object} models.ErrorResponse "编排不存在 | Orchestration not found"
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.update"]
// @Router /orchestrations/{id} [put]
// @Security BearerAuth
func (h *OrchestrationHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	orch, err := h.orchRepo.GetByIDAndTenant(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}

	var input models.OrchestrationDefinitionRequest
	if !bindOrchestrationRequest(c, &input) {
		return
	}
	input.ApplyTo(orch)

	if !h.validateOrchestrationDefinition(c, orch) {
		return
	}
	if !h.authorizeOrchestrationDefinition(c, orch, input) {
		return
	}

	if err := h.orchRepo.UpdateForTenant(orch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, orch)
}

func (h *OrchestrationHandler) authorizeOrchestrationDefinition(
	c *gin.Context,
	orch *models.Orchestration,
	definition models.OrchestrationDefinitionRequest,
) bool {
	if h.taskAuthorizationClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task authorization service is unavailable"})
		return false
	}
	definitionHash, err := definition.AuthorizationHash()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return false
	}
	if orch.AuthorizationRef == nil || *orch.AuthorizationRef == uuid.Nil {
		value := uuid.New()
		orch.AuthorizationRef = &value
	}
	token := bearerToken(c.GetHeader("Authorization"))
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return false
	}
	subject, err := h.taskAuthorizationClient.AuthorizeTaskSubject(
		c.Request.Context(), token, commonClient.TaskAuthorizationSubjectRequest{
			OwnerModule: commonExecution.ModuleOrchestrator,
			TaskType:    commonExecution.TaskTypeOrchestration,
			TaskRef:     orch.AuthorizationRef.String(), DefinitionHash: definitionHash,
		},
	)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return false
	}
	subjectID, err := strconv.ParseInt(subject.ID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "invalid task authorization subject"})
		return false
	}
	principalID, _ := strconv.ParseInt(subject.PrincipalID, 10, 64)
	membershipID, _ := strconv.ParseInt(subject.TenantMembershipID, 10, 64)
	authorizationVersion, _ := strconv.ParseInt(subject.AuthorizationVersion, 10, 64)
	orch.AuthorizationSubjectID = &subjectID
	orch.AuthorizationDefinitionHash = &definitionHash
	orch.AuthorizationPrincipalID = &principalID
	orch.AuthorizationMembershipID = &membershipID
	orch.AuthorizationVersion = &authorizationVersion
	authorizedAt := subject.AuthorizedAt.UTC()
	orch.AuthorizedAt = &authorizedAt
	return true
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], "addp_at_") {
		return ""
	}
	return parts[1]
}

func executionActorFromGin(c *gin.Context) service.ExecutionActor {
	authContext, exists := commonAuth.AuthContextFromGin(c)
	if !exists || authContext.Principal.Type != "user" || authContext.Context.TenantMembershipID == nil {
		return service.ExecutionActor{}
	}
	principalID, _ := strconv.ParseInt(authContext.Principal.ID, 10, 64)
	membershipID, _ := strconv.ParseInt(*authContext.Context.TenantMembershipID, 10, 64)
	authorizationVersion, _ := strconv.ParseInt(authContext.Authorization.AuthorizationVersion, 10, 64)
	return service.ExecutionActor{
		PrincipalID: principalID, TenantMembershipID: membershipID,
		AuthorizationVersion: authorizationVersion,
	}
}

// Delete 删除编排
// @Summary 删除编排 | Delete orchestration
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @Failure 404 {object} models.ErrorResponse "编排不存在 | Orchestration not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.delete"]
// @Router /orchestrations/{id} [delete]
// @Security BearerAuth
func (h *OrchestrationHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	if err := h.orchRepo.DeleteForTenant(uint(id), tenantID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, "orchestrator.success.deleted")})
}

// Execute 手动触发编排执行
// @Summary 执行编排 | Execute orchestration
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @Failure 404 {object} models.ErrorResponse "编排不存在 | Orchestration not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.execute"]
// @Router /orchestrations/{id}/execute [post]
// @Security BearerAuth
func (h *OrchestrationHandler) Execute(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	orch, err := h.orchRepo.GetByIDAndTenant(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}

	// 使用统一执行服务创建执行记录
	ctx := c.Request.Context()
	execution, err := h.executionService.CreateExecutionWithContext(
		ctx,
		orch.ID,
		tenantID,
		commonExecution.TriggerTypeManual,
		commonExecution.ModuleOrchestrator,
		nil,
		executionActorFromGin(c),
	)
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
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @Failure 404 {object} models.ErrorResponse "编排不存在 | Orchestration not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
// @Router /orchestrations/{id}/executions [get]
// @Security BearerAuth
func (h *OrchestrationHandler) ListExecutions(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	if _, err := h.orchRepo.GetByIDAndTenant(uint(id), tenantID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.orchestration_not_found")})
		return
	}

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
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
// @Router /executions [get]
// @Security BearerAuth
func (h *OrchestrationHandler) ListAllExecutions(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

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
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @Failure 404 {object} models.ErrorResponse "执行不存在 | Execution not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
// @Router /orch-executions/{id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) GetExecution(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, "orchestrator.error.invalid_id")})
		return
	}

	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	exec, err := h.executionService.GetExecution(ctx, uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, "orchestrator.error.execution_not_found")})
		return
	}

	c.JSON(http.StatusOK, exec)
}

// ListTaskProviders 列出所有已声明 TaskProvider 角色的模块及其当前可用性。
// @Summary 列出任务提供者 | List task providers
// @Description 从 System 模块定义读取 TaskProvider 声明，并附带当前 Backend 租约投影的动态可用性 | Read TaskProvider declarations from System module definitions with dynamic availability projected from current Backend leases
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
// @Router /task-providers [get]
// @Security BearerAuth
func (h *OrchestrationHandler) ListTaskProviders(c *gin.Context) {
	ctx := c.Request.Context()

	// 使用 TaskProviderResolver 获取所有声明了 TaskProvider 角色的模块。
	providers, err := h.taskProviderResolver.ListAllProviders(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, "orchestrator.error.list_providers_failed", err.Error())})
		return
	}

	c.JSON(http.StatusOK, providers)
}

// GetTaskProviderTaskDetail 代理读取具体 TaskProvider 任务详情及其执行契约。
// @Summary 获取具体 TaskProvider 任务详情 | Get concrete TaskProvider task detail
// @Tags TaskProvider
// @Produce json
// @Param module_name path string true "任务提供者模块名 | Task provider module name"
// @Param task_type path string true "任务类型 | Task type"
// @Param id path int true "任务 ID | Task ID"
// @Success 200 {object} map[string]interface{} "任务详情及 execution_contract | Task detail and execution_contract"
// @Failure 400 {object} models.ErrorResponse "参数无效 | Invalid parameters"
// @Failure 503 {object} models.ErrorResponse "任务提供者当前不可用 | Task provider currently unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
// @Router /task-providers/{module_name}/tasks/{task_type}/{id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) GetTaskProviderTaskDetail(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || taskID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidID)})
		return
	}
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}
	detail, err := h.taskProviderResolver.GetTaskDetail(
		c.Request.Context(), c.Param("module_name"), c.Param("task_type"), uint(taskID), tenantID,
	)
	if err != nil {
		if errors.Is(err, service.ErrTaskProviderUnavailable) {
			respondTaskProviderUnavailable(c)
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
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
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Failure 502 {object} map[string]interface{}
// @Failure 503 {object} models.ErrorResponse "任务提供者当前不可用 | Task provider currently unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
// @Router /tasks [get]
// @Security BearerAuth
func (h *OrchestrationHandler) ListModuleTasks(c *gin.Context) {
	moduleName := strings.TrimSpace(c.Query("module_name"))
	if moduleName == "" || moduleName == commonExecution.ModuleOrchestrator {
		h.ListProviderOrchestrationTasks(c)
		return
	}
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	// 1. 从 TaskProviderResolver 动态解析任务提供者和当前 Backend。
	provider, err := h.taskProviderResolver.GetProvider(ctx, moduleName)
	if err != nil {
		if errors.Is(err, service.ErrTaskProviderUnavailable) {
			respondTaskProviderUnavailable(c)
			return
		}
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
		if key != "module_name" && key != "tenant_id" && len(values) > 0 {
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

	// 4. 使用 Orchestrator 的 Tenant Service Bearer 调用 TaskProvider。
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, "orchestrator.error.create_request_failed", err.Error())})
		return
	}

	if h.serviceTokens == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, "orchestrator.error.service_auth_unavailable")})
		return
	}
	serviceToken, err := h.serviceTokens.Token(ctx, tenantID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": commoni18n.TWithDetail(c, "orchestrator.error.service_auth_failed", err.Error())})
		return
	}
	req.Header.Set("Authorization", "Bearer "+serviceToken)

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

func respondTaskProviderUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error":      commoni18n.T(c, "orchestrator.error.provider_unavailable"),
		"error_code": "task_provider_unavailable",
		"error_type": "transient",
	})
}

// ListProviderOrchestrationTasks 列出 Orchestrator 自身可编排任务。
func (h *OrchestrationHandler) ListProviderOrchestrationTasks(c *gin.Context) {
	taskType := strings.TrimSpace(c.Query("task_type"))
	if taskType != "" && taskType != commonExecution.TaskTypeOrchestration {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}

	tenantID, ok := requireTenantID(c)
	if !ok {
		return
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
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @Failure 404 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
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

	tenantID, ok := requireTenantID(c)
	if !ok {
		return
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
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.execute"]
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

	tenantID, ok := requireTenantID(c)
	if !ok {
		return
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
		executionActorFromGin(c),
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
// @Failure 403 {object} models.ErrorResponse "当前身份未绑定租户 | Current identity is not bound to a tenant"
// @Failure 404 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.read"]
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) GetProviderExecution(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
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

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	scheduler            *service.Scheduler
	engineRegistry       *service.EngineRegistry
	taskProviderRegistry *service.TaskProviderRegistry
	httpClient           *http.Client
}

// NewOrchestrationHandler 创建处理器
func NewOrchestrationHandler(
	orchRepo *repository.OrchestrationRepository,
	executionService *service.ExecutionService,
	executor *service.Executor,
	scheduler *service.Scheduler,
	engineRegistry *service.EngineRegistry,
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
		scheduler:            scheduler,
		engineRegistry:       engineRegistry,
		taskProviderRegistry: taskProviderRegistry,
		httpClient:           httpClient,
	}
}

// Create 创建编排
// @Summary 创建编排
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations [post]
// @Security BearerAuth
func (h *OrchestrationHandler) Create(c *gin.Context) {
	var req models.Orchestration
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: 从 JWT 中提取 tenant_id
	req.TenantID = 1

	if err := h.orchRepo.Create(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 如果启用且有 Cron 表达式，调度任务
	if req.Enabled && req.Schedule != "" {
		if err := h.scheduler.Schedule(req.ID, req.Schedule); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "调度失败: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusCreated, req)
}

// List 列出编排
// @Summary 获取编排列表
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
// @Summary 获取编排详情
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations/{id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	orch, err := h.orchRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "编排不存在"})
		return
	}

	c.JSON(http.StatusOK, orch)
}

// Update 更新编排
// @Summary 更新编排
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations/{id} [put]
// @Security BearerAuth
func (h *OrchestrationHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	orch, err := h.orchRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "编排不存在"})
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

	if err := h.orchRepo.Update(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 重新调度
	h.scheduler.Unschedule(req.ID)
	if req.Enabled && req.Schedule != "" {
		if err := h.scheduler.Schedule(req.ID, req.Schedule); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "调度失败: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, req)
}

// Delete 删除编排
// @Summary 删除编排
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations/{id} [delete]
// @Security BearerAuth
func (h *OrchestrationHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	h.scheduler.Unschedule(uint(id))

	if err := h.orchRepo.Delete(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// Execute 手动触发编排执行
// @Summary 执行编排
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /orchestrations/{id}/execute [post]
// @Security BearerAuth
func (h *OrchestrationHandler) Execute(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	orch, err := h.orchRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "编排不存在"})
		return
	}

	// 使用统一执行服务创建执行记录
	ctx := c.Request.Context()
	execution, err := h.executionService.CreateExecution(ctx, orch.ID, orch.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.executor.ExecuteAsync(uint(execution.ID))

	c.JSON(http.StatusAccepted, gin.H{"execution_id": execution.ID})
}

// ListExecutions 列出编排的执行记录
// @Summary 获取执行列表
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /executions [get]
// @Security BearerAuth
func (h *OrchestrationHandler) ListExecutions(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
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
// @Summary 获取执行详情
// @Tags Orchestrator
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /executions/{id} [get]
// @Security BearerAuth
func (h *OrchestrationHandler) GetExecution(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	// TODO: 从 JWT 中提取 tenant_id
	tenantID := uint(1)

	ctx := c.Request.Context()
	exec, err := h.executionService.GetExecution(ctx, uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "执行不存在"})
		return
	}

	c.JSON(http.StatusOK, exec)
}

// ListComputeEngines 列出所有任务提供者（从 System 的 task_providers 表获取）
// GET /api/compute-engines
func (h *OrchestrationHandler) ListComputeEngines(c *gin.Context) {
	ctx := c.Request.Context()

	// 使用 TaskProviderRegistry 获取所有任务提供者
	providers, err := h.taskProviderRegistry.ListAllProviders(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取任务提供者失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, providers)
}

// ListModuleTasks 列出指定模块的任务（动态调用）
// GET /api/tasks/list?module_name=transfer&page=1&page_size=100
// GET /api/tasks/list?module=transfer (向后兼容)
func (h *OrchestrationHandler) ListModuleTasks(c *gin.Context) {
	// 支持 module_name 和 module 参数(向后兼容)
	moduleName := c.Query("module_name")
	if moduleName == "" {
		moduleName = c.Query("module")
	}

	// 也支持 unique_identifier 参数,从中提取 module_name
	if moduleName == "" {
		uniqueIdentifier := c.Query("unique_identifier")
		if uniqueIdentifier != "" {
			// unique_identifier 格式: "meta.scanner.default" -> 提取第一部分 "meta"
			parts := strings.Split(uniqueIdentifier, ".")
			if len(parts) > 0 {
				moduleName = parts[0]
			}
		}
	}

	if moduleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 module_name 或 module 参数"})
		return
	}

	ctx := c.Request.Context()

	// 1. 从 TaskProviderRegistry 获取任务提供者配置
	provider, err := h.taskProviderRegistry.GetProvider(ctx, moduleName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("未找到任务提供者 %s: %v", moduleName, err)})
		return
	}

	// 2. 构建目标 URL
	if provider.BaseURL == "" || provider.TaskListEndpoint == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "任务提供者未配置完整的 API 信息"})
		return
	}

	targetURL := provider.BaseURL + provider.TaskListEndpoint

	// 3. 传递请求中的查询参数（page, page_size 等）
	queryParams := make(map[string]string)
	for key, values := range c.Request.URL.Query() {
		if key != "unique_identifier" && key != "module_name" && key != "module" && len(values) > 0 {
			queryParams[key] = values[0]
		}
	}

	// 设置默认分页参数（如果不存在）
	if _, hasPage := queryParams["page"]; !hasPage {
		queryParams["page"] = "1"
	}
	if _, hasPageSize := queryParams["page_size"]; !hasPageSize {
		queryParams["page_size"] = "100"
	}

	// 构建完整 URL
	if len(queryParams) > 0 {
		queryParts := []string{}
		for key, value := range queryParams {
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", key, value))
		}
		targetURL += "?" + strings.Join(queryParts, "&")
	}

	// 4. 发送 HTTP 请求（默认使用 GET 方法）
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("创建请求失败: %v", err)})
		return
	}

	// 传递 Authorization 头（如果存在）
	if authHeader := c.GetHeader("Authorization"); authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("调用模块 API 失败: %v", err)})
		return
	}
	defer resp.Body.Close()

	// 6. 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取响应失败: %v", err)})
		return
	}

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":       fmt.Sprintf("模块 API 返回错误，状态码: %d", resp.StatusCode),
			"target_url":  targetURL,
			"status_code": resp.StatusCode,
			"body":        string(body),
		})
		return
	}

	// 7. 解析并标准化响应格式
	var respData interface{}
	if err := json.Unmarshal(body, &respData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":      fmt.Sprintf("解析响应失败: %v", err),
			"target_url": targetURL,
			"body":       string(body),
		})
		return
	}

	// 8. 统一返回格式
	result := h.standardizeTaskListResponse(respData)
	c.JSON(http.StatusOK, result)
}

// standardizeTaskListResponse 统一任务列表响应格式
// 将不同模块的返回格式统一为 {items: [...], total: X}
// 同时确保每个任务都有 display_name 字段（用于前端中文显示）
func (h *OrchestrationHandler) standardizeTaskListResponse(respData interface{}) map[string]interface{} {
	resultMap, ok := respData.(map[string]interface{})
	if !ok {
		// 非对象响应，直接返回空列表
		return map[string]interface{}{
			"items": []interface{}{},
			"total": 0,
		}
	}

	// 情况 1: 已经是标准格式 {items: [...], total: X}
	if items, hasItems := resultMap["items"]; hasItems {
		total := 0
		if t, ok := resultMap["total"].(float64); ok {
			total = int(t)
		} else if t, ok := resultMap["total"].(int); ok {
			total = t
		}
		return map[string]interface{}{
			"items": h.ensureDisplayNames(items),
			"total": total,
		}
	}

	// 情况 2: Transfer 格式 {data: [...], total: X, page: X, page_size: X}
	if data, hasData := resultMap["data"]; hasData {
		total := 0
		if t, ok := resultMap["total"].(float64); ok {
			total = int(t)
		} else if t, ok := resultMap["total"].(int); ok {
			total = t
		}
		return map[string]interface{}{
			"items": h.ensureDisplayNames(data),
			"total": total,
		}
	}

	// 情况 3: Develop 格式 {tasks: [...], total: X, page: X, page_size: X}
	if tasks, hasTasks := resultMap["tasks"]; hasTasks {
		total := 0
		if t, ok := resultMap["total"].(float64); ok {
			total = int(t)
		} else if t, ok := resultMap["total"].(int); ok {
			total = t
		}
		return map[string]interface{}{
			"items": h.ensureDisplayNames(tasks),
			"total": total,
		}
	}

	// 情况 4: 直接是数组，包装为标准格式
	return map[string]interface{}{
		"items": []interface{}{},
		"total": 0,
	}
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


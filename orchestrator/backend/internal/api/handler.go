package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
)

// OrchestrationHandler 编排 API 处理器
type OrchestrationHandler struct {
	orchRepo       *repository.OrchestrationRepository
	execRepo       *repository.ExecutionRepository
	executor       *service.Executor
	scheduler      *service.Scheduler
	moduleClient   *service.ModuleClient
	engineRegistry *service.EngineRegistry
	httpClient     *http.Client
}

// NewOrchestrationHandler 创建处理器
func NewOrchestrationHandler(
	orchRepo *repository.OrchestrationRepository,
	execRepo *repository.ExecutionRepository,
	executor *service.Executor,
	scheduler *service.Scheduler,
	moduleClient *service.ModuleClient,
	engineRegistry *service.EngineRegistry,
	httpClient *http.Client,
) *OrchestrationHandler {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &OrchestrationHandler{
		orchRepo:       orchRepo,
		execRepo:       execRepo,
		executor:       executor,
		scheduler:      scheduler,
		moduleClient:   moduleClient,
		engineRegistry: engineRegistry,
		httpClient:     httpClient,
	}
}

// Create 创建编排
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

	execution := &models.Execution{
		OrchestrationID: orch.ID,
		TenantID:        orch.TenantID,
		Status:          "pending",
	}

	if err := h.execRepo.Create(execution); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.executor.ExecuteAsync(execution.ID)

	c.JSON(http.StatusAccepted, gin.H{"execution_id": execution.ID})
}

// ListExecutions 列出编排的执行记录
func (h *OrchestrationHandler) ListExecutions(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	execs, total, err := h.execRepo.List(uint(id), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": execs,
		"total": total,
		"limit": limit,
		"offset": offset,
	})
}

// ListAllExecutions 列出所有执行记录
func (h *OrchestrationHandler) ListAllExecutions(c *gin.Context) {
	// TODO: 从 JWT 中提取 tenant_id
	tenantID := uint(1)

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	execs, total, err := h.execRepo.ListAll(tenantID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":  execs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetExecution 获取执行详情
func (h *OrchestrationHandler) GetExecution(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	exec, err := h.execRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "执行不存在"})
		return
	}

	c.JSON(http.StatusOK, exec)
}

// ListComputeResources 列出所有具有计算能力的资源（转发到 System）
// GET /api/compute-resources
func (h *OrchestrationHandler) ListComputeResources(c *gin.Context) {
	ctx := c.Request.Context()

	// 使用 EngineRegistry 的 ListAllEngines 方法（内部会调用 System API）
	resources, err := h.engineRegistry.ListAllEngines(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取计算资源失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, resources)
}

// ListModuleTasks 列出指定资源的任务（动态调用）
// GET /api/tasks/list?unique_identifier=meta.scanner.default&page=1&page_size=100
func (h *OrchestrationHandler) ListModuleTasks(c *gin.Context) {
	uniqueIdentifier := c.Query("unique_identifier")
	if uniqueIdentifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 unique_identifier 参数"})
		return
	}

	ctx := c.Request.Context()

	// 1. 从 EngineRegistry 获取引擎配置
	engine, err := h.engineRegistry.GetEngine(ctx, uniqueIdentifier)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("未找到引擎: %v", err)})
		return
	}

	// 2. 解析 TaskAPIConfig
	if engine.TaskAPIConfig == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "引擎未配置 task_api_config"})
		return
	}

	var apiConfig commonModels.TaskAPIConfig
	if err := json.Unmarshal([]byte(*engine.TaskAPIConfig), &apiConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("解析 task_api_config 失败: %v", err)})
		return
	}

	// 3. 获取 list endpoint 配置
	listEndpoint, ok := apiConfig.Endpoints["list"]
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "引擎未配置 list endpoint"})
		return
	}

	// 4. 构建目标 URL（支持查询参数模板替换）
	targetURL := apiConfig.BaseURL + listEndpoint.Path

	// 构建查询参数（从模板中提取 + 从请求中传递）
	queryParams := make(map[string]string)

	// 从模板中提取查询参数并替换变量
	if listEndpoint.QueryParams != nil {
		for key, templateValue := range listEndpoint.QueryParams {
			// 简单模板替换（支持 {{.TenantID}} 等变量）
			replacedValue := replaceTemplateVars(templateValue, c)
			queryParams[key] = replacedValue
		}
	}

	// 传递请求中的查询参数（page, page_size 等）
	for key, values := range c.Request.URL.Query() {
		if key != "unique_identifier" && len(values) > 0 {
			queryParams[key] = values[0]
		}
	}

	// 构建完整 URL
	if len(queryParams) > 0 {
		queryParts := []string{}
		for key, value := range queryParams {
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", key, value))
		}
		targetURL += "?" + strings.Join(queryParts, "&")
	}

	// 5. 发送 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, listEndpoint.Method, targetURL, nil)
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


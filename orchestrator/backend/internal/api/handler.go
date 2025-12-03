package api

import (
	"net/http"
	"strconv"

	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
)

// OrchestrationHandler 编排 API 处理器
type OrchestrationHandler struct {
	orchRepo     *repository.OrchestrationRepository
	execRepo     *repository.ExecutionRepository
	executor     *service.Executor
	scheduler    *service.Scheduler
	moduleClient *service.ModuleClient
}

// NewOrchestrationHandler 创建处理器
func NewOrchestrationHandler(
	orchRepo *repository.OrchestrationRepository,
	execRepo *repository.ExecutionRepository,
	executor *service.Executor,
	scheduler *service.Scheduler,
	moduleClient *service.ModuleClient,
) *OrchestrationHandler {
	return &OrchestrationHandler{
		orchRepo:     orchRepo,
		execRepo:     execRepo,
		executor:     executor,
		scheduler:    scheduler,
		moduleClient: moduleClient,
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
	if req.Enabled && req.CronExpr != "" {
		if err := h.scheduler.Schedule(req.ID, req.CronExpr); err != nil {
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
	if req.Enabled && req.CronExpr != "" {
		if err := h.scheduler.Schedule(req.ID, req.CronExpr); err != nil {
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

// ListModuleTasks 列出指定模块的任务
// GET /api/tasks/list?module=transfer|meta|manager&page=1&page_size=100
func (h *OrchestrationHandler) ListModuleTasks(c *gin.Context) {
	module := c.Query("module")
	if module == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 module 参数"})
		return
	}

	var result interface{}
	var err error

	switch module {
	case "transfer":
		result, err = h.listTransferTasks(c)
	case "meta":
		result, err = h.listMetaTasks(c)
	case "manager":
		result, err = h.listManagerTasks(c)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的模块名称"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *OrchestrationHandler) listTransferTasks(c *gin.Context) (interface{}, error) {
	page := c.DefaultQuery("page", "1")
	pageSize := c.DefaultQuery("page_size", "100")

	params := map[string]interface{}{}
	if page != "" {
		params["page"] = page
	}
	if pageSize != "" {
		params["page_size"] = pageSize
	}
	if taskType := c.Query("type"); taskType != "" {
		params["type"] = taskType
	}
	if status := c.Query("status"); status != "" {
		params["status"] = status
	}

	return h.moduleClient.Call(c, "transfer", "/api/tasks", "GET", params)
}

func (h *OrchestrationHandler) listMetaTasks(c *gin.Context) (interface{}, error) {
	return h.moduleClient.Call(c, "meta", "/api/meta/scan/tasks", "GET", nil)
}

func (h *OrchestrationHandler) listManagerTasks(c *gin.Context) (interface{}, error) {
	// Manager 模块暂无通用任务列表API,返回空列表
	return map[string]interface{}{
		"items": []interface{}{},
		"total": 0,
	}, nil
}


package api

import (
	"net/http"
	"strconv"

	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// DevTaskHandler 开发任务API处理器
type DevTaskHandler struct {
	devTaskService *service.DevTaskService
}

// NewDevTaskHandler 创建开发任务处理器
func NewDevTaskHandler(devTaskService *service.DevTaskService) *DevTaskHandler {
	return &DevTaskHandler{
		devTaskService: devTaskService,
	}
}

// CreateDevTask 创建开发任务
// @Summary 创建开发任务 | Create development task
// @Tags DevTask
// @Accept json
// @Produce json
// @Param body body models.CreateDevTaskSwaggerRequest true "创建请求 | Create request"
// @Success 200 {object} models.DevTaskSwagger "已创建的开发任务 | Created development task"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.create"]
// @Router /task-definitions [post]
func (h *DevTaskHandler) CreateDevTask(c *gin.Context) {
	var req models.CreateDevTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := tenantIDValue(c)
	userID := userIDValue(c)

	item, err := h.devTaskService.CreateDevTask(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// UpdateDevTask 更新开发任务
// @Summary 更新开发任务 | Update development task
// @Tags DevTask
// @Accept json
// @Produce json
// @Param id path int true "开发任务 ID | Development task ID"
// @Param body body models.UpdateDevTaskSwaggerRequest true "更新请求 | Update request"
// @Success 200 {object} models.DevTaskSwagger "已更新的开发任务 | Updated development task"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.update"]
// @Router /task-definitions/{id} [put]
func (h *DevTaskHandler) UpdateDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var req models.UpdateDevTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := tenantIDValue(c)
	userID := userIDValue(c)

	item, err := h.devTaskService.UpdateDevTask(uint(id), &req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetDevTask 获取开发任务详情
// @Summary 获取开发任务详情 | Get development task details
// @Tags DevTask
// @Produce json
// @Param id path int true "开发任务ID | Development task ID"
// @Success 200 {object} models.DevTaskSwagger "开发任务详情 | Development task details"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /task-definitions/{id} [get]
func (h *DevTaskHandler) GetDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := tenantIDValue(c)

	item, err := h.devTaskService.GetDevTask(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// ProviderGetDevTask 按 TaskProvider 标准路径获取开发任务详情。
// @Summary 获取 TaskProvider 开发任务详情 | Get TaskProvider development task detail
// @Description 按标准 TaskProvider 路径获取开发任务详情；task_type 是对外任务类型契约，映射到 Develop 内部 dev_type。| Get development task detail through the standard TaskProvider path; task_type is the external task contract mapped to Develop internal dev_type.
// @Tags DevTask
// @Produce json
// @Param task_type path string true "TaskProvider 任务类型：query/workflow/script | TaskProvider task type: query/workflow/script"
// @Param id path int true "开发任务ID | Development task ID"
// @Success 200 {object} models.ProviderDevTaskSwagger "开发任务详情 | Development task detail"
// @Failure 400 {object} map[string]interface{} "参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @x-addp-auth-mode "internal"
// @Router /internal/tasks/{task_type}/{id} [get]
func (h *DevTaskHandler) ProviderGetDevTask(c *gin.Context) {
	taskType := c.Param("task_type")
	if !isDevelopTaskType(taskType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := internalTenantIDValue(c)
	item, err := h.devTaskService.GetDevTask(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if item.DevType != taskType {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found for task_type: " + taskType})
		return
	}

	c.JSON(http.StatusOK, models.NewProviderDevTask(*item))
}

func isDevelopTaskType(taskType string) bool {
	switch taskType {
	case commonExecution.TaskTypeQuery, commonExecution.TaskTypeWorkflow, commonExecution.TaskTypeScript:
		return true
	default:
		return false
	}
}

// ListDevTasks 查询开发任务列表
// @Summary 查询开发任务列表 | List development tasks
// @Tags DevTask
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param dev_type query string false "Develop 内部类型过滤：query/workflow/script | Develop internal type filter: query/workflow/script"
// @Param status query string false "状态过滤 | Filter by status"
// @Param engine_id query int false "资源ID过滤 | Filter by engine ID"
// @Param tag query string false "标签过滤 | Filter by tag"
// @Param keyword query string false "关键词搜索 | Keyword search"
// @Success 200 {object} models.ListDevTasksSwaggerResponse "开发任务列表 | Development task list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /task-definitions [get]
func (h *DevTaskHandler) ListDevTasks(c *gin.Context) {
	var req models.ListDevTasksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := tenantIDValue(c)

	items, total, err := h.devTaskService.ListDevTasks(&req, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 设置默认分页参数
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 20
	}

	c.JSON(http.StatusOK, models.ListDevTasksResponse{
		Items:    items,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
}

// DeleteDevTask 删除开发任务
// @Summary 删除开发任务 | Delete development task
// @Tags DevTask
// @Param id path int true "开发任务ID | Development task ID"
// @Success 200 {object} map[string]string "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.delete"]
// @Router /task-definitions/{id} [delete]
func (h *DevTaskHandler) DeleteDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := tenantIDValue(c)

	if err := h.devTaskService.DeleteDevTask(uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, developi18n.MsgDeleteSuccess)})
}

// ExecuteDevTask 执行开发任务
// @Summary 执行开发任务 | Execute development task
// @Tags DevTask
// @Accept json
// @Produce json
// @Param id path int true "开发任务ID | Development task ID"
// @Param body body map[string]interface{} false "执行参数 | Execution parameters"
// @Success 200 {object} map[string]string "执行已启动 | Execution started"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute"]
// @Router /task-definitions/{id}/execute [post]
func (h *DevTaskHandler) ExecuteDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	// 实际执行逻辑在 execution_handler 中
	// 这里返回提示信息
	c.JSON(http.StatusOK, gin.H{
		"message": commoni18n.T(c, developi18n.MsgUseExecuteEndpoint),
		"task_id": id,
	})
}

// GetDevTaskStatistics 获取开发任务统计
// @Summary 获取开发任务统计 | Get development task statistics
// @Tags DevTask
// @Produce json
// @Success 200 {object} map[string]int64 "统计数据 | Statistics"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /task-definitions/statistics [get]
func (h *DevTaskHandler) GetDevTaskStatistics(c *gin.Context) {
	tenantID := tenantIDValue(c)

	stats, err := h.devTaskService.CountByType(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

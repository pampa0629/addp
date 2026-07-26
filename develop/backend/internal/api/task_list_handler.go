package api

import (
	"net/http"
	"strconv"
	"strings"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskListHandler TaskProvider 任务列表 API
type TaskListHandler struct {
	devTaskService *service.DevTaskService
}

// NewTaskListHandler 创建任务列表处理器
func NewTaskListHandler(devTaskService *service.DevTaskService) *TaskListHandler {
	return &TaskListHandler{
		devTaskService: devTaskService,
	}
}

// ListTasks 查询 TaskProvider 任务列表
// @Summary 列出可编排任务 | List orchestratable tasks
// @Description 返回可供 TaskProvider 编排复用的开发任务；task_type 是对外任务类型契约，映射到 Develop 内部 dev_type。| List active develop tasks exposed by TaskProvider; task_type is the external task contract mapped to Develop internal dev_type.
// @Tags Develop
// @Produce json
// @Param task_type query string false "TaskProvider 任务类型：query/workflow/script | TaskProvider task type: query/workflow/script"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} models.ListProviderDevTasksSwaggerResponse
// @Failure 500 {object} map[string]interface{}
// @x-addp-auth-mode "internal"
// @Router /internal/tasks [get]
func (h *TaskListHandler) ListTasks(c *gin.Context) {
	tenantID := internalTenantIDValue(c)
	taskType := strings.TrimSpace(c.Query("task_type"))
	if taskType != "" && !isDevelopTaskType(taskType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}

	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// 构建查询请求（查询所有类型，后续过滤）
	req := &models.ListDevTasksRequest{
		Page:     page,
		PageSize: pageSize,
		Status:   "active", // 仅返回活跃任务
	}
	if taskType != "" {
		req.DevType = taskType
	}

	// 查询所有活跃的 DevTask
	allItems, _, err := h.devTaskService.ListDevTasks(req, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 过滤：只保留可编排的开发任务类型
	filteredItems := []models.DevTask{}
	for _, item := range allItems {
		if item.DevType == commonExecution.TaskTypeQuery ||
			item.DevType == commonExecution.TaskTypeWorkflow ||
			item.DevType == commonExecution.TaskTypeScript {
			filteredItems = append(filteredItems, item)
		}
	}

	// 返回标准任务列表响应
	c.JSON(http.StatusOK, models.ListProviderDevTasksResponse{
		Items:    models.NewProviderDevTasks(filteredItems),
		Total:    int64(len(filteredItems)),
		Page:     page,
		PageSize: pageSize,
	})
}

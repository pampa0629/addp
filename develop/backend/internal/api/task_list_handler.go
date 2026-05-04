package api

import (
	"net/http"
	"strconv"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskListHandler 任务列表 API（供 Orchestrator 使用）
type TaskListHandler struct {
	devTaskService *service.DevTaskService
}

// NewTaskListHandler 创建任务列表处理器
func NewTaskListHandler(devTaskService *service.DevTaskService) *TaskListHandler {
	return &TaskListHandler{
		devTaskService: devTaskService,
	}
}

// ListTasks 查询任务列表（Orchestrator 标准接口）
// @Summary 列出可编排任务 | List orchestratable tasks
// @Description 返回可供 Orchestrator 编排复用的开发任务 | List active develop tasks for Orchestrator
// @Tags Develop
// @Produce json
// @Param unique_identifier query string false "任务提供者唯一标识 | Task provider identifier"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /tasks/list [get]
// @Security BearerAuth
func (h *TaskListHandler) ListTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// 构建查询请求（查询所有类型，后续过滤）
	req := &models.ListDevTasksRequest{
		Page:     page,
		PageSize: pageSize,
		Status:   "active", // 仅返回活跃任务
		// 不指定 DevType，查询所有类型
	}

	// 查询所有活跃的 DevItem
	allItems, _, err := h.devTaskService.ListDevItems(req, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 过滤：只保留 sql 和 workflow，排除 script
	filteredItems := []models.DevTask{}
	for _, item := range allItems {
		if item.DevType == "sql" || item.DevType == "workflow" {
			filteredItems = append(filteredItems, item)
		}
	}

	// 返回响应（Orchestrator 期望的格式）
	c.JSON(http.StatusOK, gin.H{
		"items":     filteredItems,
		"total":     len(filteredItems), // 过滤后的总数
		"page":      page,
		"page_size": pageSize,
	})
}

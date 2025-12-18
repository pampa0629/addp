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
	devItemService *service.DevItemService
}

// NewTaskListHandler 创建任务列表处理器
func NewTaskListHandler(devItemService *service.DevItemService) *TaskListHandler {
	return &TaskListHandler{
		devItemService: devItemService,
	}
}

// ListTasks 查询任务列表（Orchestrator 标准接口）
// GET /api/develop/tasks/list?unique_identifier=develop.default&page=1&page_size=20
func (h *TaskListHandler) ListTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// 构建查询请求（查询所有类型，后续过滤）
	req := &models.ListDevItemsRequest{
		Page:     page,
		PageSize: pageSize,
		Status:   "active", // 仅返回活跃任务
		// 不指定 DevType，查询所有类型
	}

	// 查询所有活跃的 DevItem
	allItems, _, err := h.devItemService.ListDevItems(req, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 过滤：只保留 sql 和 workflow，排除 script
	filteredItems := []models.DevItem{}
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

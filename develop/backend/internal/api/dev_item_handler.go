package api

import (
	"net/http"
	"strconv"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	developi18n "github.com/addp/develop/backend/i18n"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

// DevItemHandler 开发项API处理器
type DevItemHandler struct {
	devTaskService *service.DevTaskService
}

// NewDevItemHandler 创建开发项处理器
func NewDevItemHandler(devTaskService *service.DevTaskService) *DevItemHandler {
	return &DevItemHandler{
		devTaskService: devTaskService,
	}
}

// CreateDevItem 创建开发项
// @Summary 创建开发项 | Create development item
// @Tags DevItem
// @Accept json
// @Produce json
// @Param body body models.CreateDevTaskRequest true "创建请求 | Create request"
// @Success 200 {object} models.DevTask "已创建的开发项 | Created development item"
// @Router /items [post]
func (h *DevItemHandler) CreateDevItem(c *gin.Context) {
	var req models.CreateDevTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	item, err := h.devTaskService.CreateDevItem(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// UpdateDevItem 更新开发项
// @Summary 更新开发项 | Update development item
// @Tags DevItem
// @Accept json
// @Produce json
// @Param id path int true "开发项ID | Development item ID"
// @Param body body models.UpdateDevTaskRequest true "更新请求 | Update request"
// @Success 200 {object} models.DevTask "已更新的开发项 | Updated development item"
// @Router /items/{id} [put]
func (h *DevItemHandler) UpdateDevItem(c *gin.Context) {
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

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	item, err := h.devTaskService.UpdateDevItem(uint(id), &req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetDevItem 获取开发项详情
// @Summary 获取开发项详情 | Get development item details
// @Tags DevItem
// @Produce json
// @Param id path int true "开发项ID | Development item ID"
// @Success 200 {object} models.DevTask "开发项详情 | Development item details"
// @Router /items/{id} [get]
func (h *DevItemHandler) GetDevItem(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	item, err := h.devTaskService.GetDevItem(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// ListDevItems 查询开发项列表
// @Summary 查询开发项列表 | List development items
// @Tags DevItem
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param dev_type query string false "类型过滤 | Filter by type"
// @Param status query string false "状态过滤 | Filter by status"
// @Param engine_id query int false "资源ID过滤 | Filter by engine ID"
// @Param tag query string false "标签过滤 | Filter by tag"
// @Param keyword query string false "关键词搜索 | Keyword search"
// @Success 200 {object} models.ListDevTasksResponse "开发项列表 | Development item list"
// @Router /items [get]
func (h *DevItemHandler) ListDevItems(c *gin.Context) {
	var req models.ListDevTasksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")

	items, total, err := h.devTaskService.ListDevItems(&req, tenantID)
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

// DeleteDevItem 删除开发项
// @Summary 删除开发项 | Delete development item
// @Tags DevItem
// @Param id path int true "开发项ID | Development item ID"
// @Success 200 {object} map[string]string "删除成功 | Deleted successfully"
// @Router /items/{id} [delete]
func (h *DevItemHandler) DeleteDevItem(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.devTaskService.DeleteDevItem(uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, developi18n.MsgDeleteSuccess)})
}

// ExecuteDevItem 执行开发项
// @Summary 执行开发项 | Execute development item
// @Tags DevItem
// @Accept json
// @Produce json
// @Param id path int true "开发项ID | Development item ID"
// @Param body body map[string]interface{} false "执行参数 | Execution parameters"
// @Success 200 {object} map[string]string "执行已启动 | Execution started"
// @Router /items/{id}/execute [post]
func (h *DevItemHandler) ExecuteDevItem(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	// 实际执行逻辑在 dev_execution_handler 中
	// 这里返回提示信息
	c.JSON(http.StatusOK, gin.H{
		"message":     commoni18n.T(c, developi18n.MsgUseExecuteEndpoint),
		"dev_item_id": id,
	})
}

// GetDevItemStatistics 获取开发项统计
// @Summary 获取开发项统计 | Get development item statistics
// @Tags DevItem
// @Produce json
// @Success 200 {object} map[string]int64 "统计数据 | Statistics"
// @Router /items/statistics [get]
func (h *DevItemHandler) GetDevItemStatistics(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	stats, err := h.devTaskService.CountByType(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

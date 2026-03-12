package api

import (
	"net/http"
	"strconv"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
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
// @Summary 创建开发项
// @Tags DevItem
// @Accept json
// @Produce json
// @Param body body models.CreateDevTaskRequest true "创建请求"
// @Success 200 {object} models.DevItem
// @Router /api/develop/items [post]
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
// @Summary 更新开发项
// @Tags DevItem
// @Accept json
// @Produce json
// @Param id path int true "开发项ID"
// @Param body body models.UpdateDevTaskRequest true "更新请求"
// @Success 200 {object} models.DevItem
// @Router /api/develop/items/{id} [put]
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
// @Summary 获取开发项详情
// @Tags DevItem
// @Produce json
// @Param id path int true "开发项ID"
// @Success 200 {object} models.DevItem
// @Router /api/develop/items/{id} [get]
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
// @Summary 查询开发项列表
// @Tags DevItem
// @Produce json
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Param dev_type query string false "类型过滤"
// @Param status query string false "状态过滤"
// @Param engine_id query int false "资源ID过滤"
// @Param tag query string false "标签过滤"
// @Param keyword query string false "关键词搜索"
// @Success 200 {object} models.ListDevTasksResponse
// @Router /api/develop/items [get]
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
// @Summary 删除开发项
// @Tags DevItem
// @Param id path int true "开发项ID"
// @Success 200 {object} map[string]string
// @Router /api/develop/items/{id} [delete]
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

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// ExecuteDevItem 执行开发项
// @Summary 执行开发项
// @Tags DevItem
// @Accept json
// @Produce json
// @Param id path int true "开发项ID"
// @Param body body map[string]interface{} false "执行参数"
// @Success 200 {object} map[string]string
// @Router /api/develop/items/{id}/execute [post]
func (h *DevItemHandler) ExecuteDevItem(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	// 实际执行逻辑在 dev_execution_handler 中
	// 这里返回提示信息
	c.JSON(http.StatusOK, gin.H{
		"message":     "请使用 POST /api/develop/items/{id}/execute 执行开发项",
		"dev_item_id": id,
	})
}

// GetDevItemStatistics 获取开发项统计
// @Summary 获取开发项统计
// @Tags DevItem
// @Produce json
// @Success 200 {object} map[string]int64
// @Router /api/develop/items/statistics [get]
func (h *DevItemHandler) GetDevItemStatistics(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	stats, err := h.devTaskService.CountByType(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

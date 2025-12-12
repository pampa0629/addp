package api

import (
	"net/http"
	"strconv"

	"github.com/addp/develop/internal/models"
	"github.com/addp/develop/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SpatialHandler 空间工作流处理器
type SpatialHandler struct {
	db                     *gorm.DB
	spatialWorkflowService *service.SpatialWorkflowService
}

// NewSpatialHandler 创建空间工作流处理器
func NewSpatialHandler(db *gorm.DB, spatialWorkflowService *service.SpatialWorkflowService) *SpatialHandler {
	return &SpatialHandler{
		db:                     db,
		spatialWorkflowService: spatialWorkflowService,
	}
}

// ExecuteWorkflow 即时执行工作流
// POST /api/spatial/workflow/execute
func (h *SpatialHandler) ExecuteWorkflow(c *gin.Context) {
	var req struct {
		WorkflowDef map[string]interface{} `json:"workflow_def" binding:"required"`
		InputData   map[string]interface{} `json:"input_data"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 从上下文获取用户信息
	userID, _ := c.Get("user_id")
	tenantID, _ := c.Get("tenant_id")

	// 调用 GeoPandas Engine 执行工作流
	result, err := h.spatialWorkflowService.ExecuteWorkflow(c.Request.Context(), req.WorkflowDef, req.InputData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// TODO: 将结果存储到 PostGIS (develop.spatial_execution_results)
	_ = userID
	_ = tenantID

	c.JSON(http.StatusOK, result)
}

// ExecuteOperator 执行单个算子
// POST /api/spatial/operators/:name/execute
func (h *SpatialHandler) ExecuteOperator(c *gin.Context) {
	operatorName := c.Param("name")

	var req struct {
		Params map[string]interface{} `json:"params" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 调用 GeoPandas Engine 执行算子
	result, err := h.spatialWorkflowService.ExecuteOperator(c.Request.Context(), operatorName, req.Params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ListOperators 获取所有算子列表
// GET /api/spatial/operators
func (h *SpatialHandler) ListOperators(c *gin.Context) {
	// 调用 GeoPandas Engine 获取算子列表
	operators, err := h.spatialWorkflowService.ListOperators(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"operators": operators,
		"count":     len(operators),
	})
}

// CreateTask 创建 GIS 任务（保存工作流为任务）
// POST /api/spatial/tasks
func (h *SpatialHandler) CreateTask(c *gin.Context) {
	var req struct {
		Name         string                  `json:"name" binding:"required"`
		Description  string                  `json:"description"`
		WorkflowDef  models.WorkflowDef      `json:"workflow_def" binding:"required"`
		InputSchema  *models.InputSchema     `json:"input_schema"`
		OutputSchema *models.OutputSchema    `json:"output_schema"`
		Schedule     string                  `json:"schedule"` // Cron 表达式
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 从上下文获取用户信息
	userID, _ := c.Get("user_id")
	tenantID, _ := c.Get("tenant_id")

	// 创建任务
	task := models.SpatialTask{
		TenantID:     tenantID.(uint),
		Name:         req.Name,
		Description:  req.Description,
		WorkflowDef:  req.WorkflowDef,
		InputSchema:  req.InputSchema,
		OutputSchema: req.OutputSchema,
		Schedule:     req.Schedule,
		Status:       "active",
		CreatedBy:    userID.(uint),
	}

	if err := h.db.Create(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"task_id": task.ID,
		"task":    task,
	})
}

// ListTasks 获取任务列表
// GET /api/spatial/tasks
func (h *SpatialHandler) ListTasks(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")

	// 分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize

	// 查询任务
	var tasks []models.SpatialTask
	var total int64

	query := h.db.Where("tenant_id = ?", tenantID).Where("deleted_at IS NULL")

	// 获取总数
	if err := query.Model(&models.SpatialTask{}).Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 获取分页数据
	if err := query.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"tasks":     tasks,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetTask 获取任务详情
// GET /api/spatial/tasks/:id
func (h *SpatialHandler) GetTask(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	var task models.SpatialTask
	if err := h.db.Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", taskID, tenantID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"task":   task,
	})
}

// UpdateTask 更新任务
// PUT /api/spatial/tasks/:id
func (h *SpatialHandler) UpdateTask(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	// 查找任务
	var task models.SpatialTask
	if err := h.db.Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", taskID, tenantID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 绑定更新数据
	var req struct {
		Name         *string                 `json:"name"`
		Description  *string                 `json:"description"`
		WorkflowDef  *models.WorkflowDef     `json:"workflow_def"`
		InputSchema  *models.InputSchema     `json:"input_schema"`
		OutputSchema *models.OutputSchema    `json:"output_schema"`
		Schedule     *string                 `json:"schedule"`
		Status       *string                 `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 更新字段
	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.WorkflowDef != nil {
		updates["workflow_def"] = *req.WorkflowDef
	}
	if req.InputSchema != nil {
		updates["input_schema"] = *req.InputSchema
	}
	if req.OutputSchema != nil {
		updates["output_schema"] = *req.OutputSchema
	}
	if req.Schedule != nil {
		updates["schedule"] = *req.Schedule
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	if err := h.db.Model(&task).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 重新查询更新后的任务
	h.db.Where("id = ?", taskID).First(&task)

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"task":   task,
	})
}

// DeleteTask 删除任务（软删除）
// DELETE /api/spatial/tasks/:id
func (h *SpatialHandler) DeleteTask(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	// 软删除
	if err := h.db.Where("id = ? AND tenant_id = ?", taskID, tenantID).Delete(&models.SpatialTask{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Task deleted successfully",
	})
}

// ExecuteTask 执行已保存的任务
// POST /api/spatial/tasks/:id/execute
func (h *SpatialHandler) ExecuteTask(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	// 查找任务
	var task models.SpatialTask
	if err := h.db.Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", taskID, tenantID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 绑定输入参数
	var req struct {
		Inputs map[string]interface{} `json:"inputs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 构建工作流定义（从任务的 WorkflowDef）
	workflowDef := make(map[string]interface{})
	workflowDefBytes, _ := task.WorkflowDef.Value()
	_ = task.WorkflowDef.Scan(workflowDefBytes)

	// 转换为 map[string]interface{}
	workflowDef["tasks"] = task.WorkflowDef.Tasks

	// 执行工作流
	result, err := h.spatialWorkflowService.ExecuteWorkflow(c.Request.Context(), workflowDef, req.Inputs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 更新任务的最后执行信息
	executionID, _ := uuid.Parse(result.ExecutionID)
	h.db.Model(&task).Updates(map[string]interface{}{
		"last_execution_id":         executionID,
		"last_execution_status":     result.Status,
		"last_execution_started_at": gorm.Expr("NOW()"),
	})

	c.JSON(http.StatusOK, result)
}

// GetExecutionStatus 查询执行状态
// GET /api/spatial/executions/:id
func (h *SpatialHandler) GetExecutionStatus(c *gin.Context) {
	executionID := c.Param("id")

	// 查询执行状态
	status, err := h.spatialWorkflowService.GetTaskStatus(c.Request.Context(), executionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, status)
}

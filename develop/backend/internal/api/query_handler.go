package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// QueryHandler 查询开发 API 处理器
type QueryHandler struct {
	sqlEngine      *service.SQLEngineService
	devTaskService *service.DevTaskService
}

// NewQueryHandler 创建 查询处理器
func NewQueryHandler(sqlEngine *service.SQLEngineService, devTaskService *service.DevTaskService) *QueryHandler {
	return &QueryHandler{
		sqlEngine:      sqlEngine,
		devTaskService: devTaskService,
	}
}

// TestConnectionRequest 测试连接请求
type TestConnectionRequest struct {
	EngineID uint `json:"engine_id" binding:"required"`
}

// ExecuteQueryRequest 执行 查询请求
type ExecuteQueryRequest struct {
	EngineID uint   `json:"engine_id" binding:"required"`
	Query    string `json:"query" binding:"required"`
	Timeout  int    `json:"timeout"` // 超时时间（秒）
}

// ExecuteQueryResponse 执行 查询响应
type ExecuteQueryResponse struct {
	Columns         []string                 `json:"columns"`
	Rows            []map[string]interface{} `json:"rows"`
	RowsCount       int                      `json:"rows_count"`
	RowsAffected    int64                    `json:"rows_affected"`
	ExecutionTimeMs int64                    `json:"execution_time_ms"`
}

// SaveQueryTaskRequest 保存 查询任务请求
type SaveQueryTaskRequest struct {
	Name        string   `json:"name" binding:"required"`
	DisplayName string   `json:"display_name"`
	EngineID    uint     `json:"engine_id" binding:"required"`
	Query       string   `json:"query" binding:"required"`
	QueryType   string   `json:"query_type"` // sql, mql, dsl (从前端传递或自动推断)
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Schedule    string   `json:"schedule"`     // Cron 表达式
	IsScheduled bool     `json:"is_scheduled"` // 是否启用调度
	Timeout     int      `json:"timeout"`      // 超时时间（秒）
}

// TestConnection 测试数据源连接
// @Summary 测试数据源连接
// @Tags Query
// @Accept json
// @Produce json
// @Param id path int true "资源ID"
// @Success 200 {object} map[string]string
// @Router /api/develop/test/{id} [get]
func (h *QueryHandler) TestConnection(c *gin.Context) {
	idStr := c.Param("id")
	engineID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	// 测试连接
	if err := h.sqlEngine.TestConnection(uint(engineID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "连接测试失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "连接测试成功",
	})
}

// ExecuteQuery 执行 查询语句
// @Summary 执行 查询语句
// @Tags Query
// @Accept json
// @Produce json
// @Param body body ExecuteQueryRequest true "查询请求"
// @Success 200 {object} ExecuteQueryResponse
// @Router /api/develop/execute [post]
func (h *QueryHandler) ExecuteQuery(c *gin.Context) {
	var req ExecuteQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 验证查询内容
	sql := strings.TrimSpace(req.Query)
	if sql == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "查询语句不能为空"})
		return
	}

	// 判断是查询还是 DML
	sqlLower := strings.ToLower(sql)
	isQuery := strings.HasPrefix(sqlLower, "select") ||
		strings.HasPrefix(sqlLower, "show") ||
		strings.HasPrefix(sqlLower, "desc") ||
		strings.HasPrefix(sqlLower, "explain")

	// 设置默认超时
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 // 默认 30 秒
	}

	// 执行 SQL
	if isQuery {
		// 查询语句
		result, err := h.sqlEngine.ExecuteSQL(c.Request.Context(), req.EngineID, sql, timeout)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "查询执行失败",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, ExecuteQueryResponse{
			Columns:      result.Columns,
			Rows:         result.Rows,
			RowsCount:    len(result.Rows),
			RowsAffected: result.RowsAffected,
		})
	} else {
		// DML 语句 (INSERT/UPDATE/DELETE)
		rowsAffected, err := h.sqlEngine.ExecuteDML(c.Request.Context(), req.EngineID, sql, timeout)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "查询执行失败",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, ExecuteQueryResponse{
			Columns:      []string{},
			Rows:         []map[string]interface{}{},
			RowsCount:    0,
			RowsAffected: rowsAffected,
		})
	}
}

// SaveQueryTask 保存 SQL 为任务
// @Summary 保存 SQL 为任务
// @Tags Query
// @Accept json
// @Produce json
// @Param body body SaveQueryTaskRequest true "保存请求"
// @Success 200 {object} models.DevItem
// @Router /api/develop/sql/tasks [post]
func (h *QueryHandler) SaveQueryTask(c *gin.Context) {
	var req SaveQueryTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	// 构建 DevItem 请求
	// 如果前端没有传递query_type，默认设为"sql"
	queryType := req.QueryType
	if queryType == "" {
		queryType = "sql"
	}

	createReq := &models.CreateDevTaskRequest{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		DevType:     "query",
		Content: map[string]interface{}{
			"query":      req.Query,
			"query_type": queryType,
		},
		ExecutionConfig: map[string]interface{}{
			"engine_id": req.EngineID,
		},
		Schedule:    req.Schedule,
		Timeout:     req.Timeout,
		Description: req.Description,
		Tags:        req.Tags,
	}

	// 设置默认超时
	if createReq.Timeout <= 0 {
		createReq.Timeout = 300 // 5分钟
	}

	// 创建开发项
	item, err := h.devTaskService.CreateDevItem(createReq, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "保存 查询任务失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, item)
}

// UpdateQueryTask 更新 查询任务
// @Summary 更新 查询任务
// @Tags Query
// @Accept json
// @Produce json
// @Param id path int true "任务ID"
// @Param body body SaveQueryTaskRequest true "更新请求"
// @Success 200 {object} models.DevItem
// @Router /api/develop/sql/tasks/{id} [put]
func (h *QueryHandler) UpdateQueryTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	var req SaveQueryTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	// 构建更新请求
	updateReq := &models.UpdateDevTaskRequest{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Content: map[string]interface{}{
			"query":      req.Query,
			"query_type": req.QueryType,
		},
		ExecutionConfig: map[string]interface{}{
			"engine_id": req.EngineID,
		},
		Schedule:    req.Schedule,
		Timeout:     req.Timeout,
		Description: req.Description,
		Tags:        req.Tags,
	}

	// 更新开发项
	item, err := h.devTaskService.UpdateDevItem(uint(id), updateReq, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "更新 查询任务失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetQueryTask 获取 查询任务详情
// @Summary 获取 查询任务详情
// @Tags Query
// @Produce json
// @Param id path int true "任务ID"
// @Success 200 {object} models.DevItem
// @Router /api/develop/sql/tasks/{id} [get]
func (h *QueryHandler) GetQueryTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	item, err := h.devTaskService.GetDevItem(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// 验证是否为 SQL 类型
	if item.DevType != "query" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该任务不是 SQL 类型"})
		return
	}

	c.JSON(http.StatusOK, item)
}

// ListQueryTasks 获取 查询任务列表
// @Summary 获取 查询任务列表
// @Tags Query
// @Produce json
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Param status query string false "状态过滤"
// @Param keyword query string false "关键词搜索"
// @Success 200 {object} models.ListDevTasksResponse
// @Router /api/develop/sql/tasks [get]
func (h *QueryHandler) ListQueryTasks(c *gin.Context) {
	var req models.ListDevTasksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 强制过滤 SQL 类型
	req.DevType = "query"

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

// DeleteQueryTask 删除 查询任务
// @Summary 删除 查询任务
// @Tags Query
// @Param id path int true "任务ID"
// @Success 200 {object} map[string]string
// @Router /api/develop/sql/tasks/{id} [delete]
func (h *QueryHandler) DeleteQueryTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.devTaskService.DeleteDevItem(uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

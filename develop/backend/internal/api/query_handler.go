package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// QueryHandler 查询开发 API 处理器
type QueryHandler struct {
	sqlEngine *service.SQLEngineService
}

// NewQueryHandler 创建 查询处理器
func NewQueryHandler(sqlEngine *service.SQLEngineService) *QueryHandler {
	return &QueryHandler{
		sqlEngine: sqlEngine,
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
	GraphData       *plugin.GraphData        `json:"graph_data,omitempty"` // 图数据（仅图数据库引擎）
}

// GetSampleQuery 获取引擎的可执行样例查询（切换引擎时自动填充编辑器）
// @Summary 获取样例查询 | Get sample query
// @Tags Query
// @Produce json
// @Param id path int true "引擎ID | Engine ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /engines/{id}/sample-query [get]
// @Security BearerAuth
func (h *QueryHandler) GetSampleQuery(c *gin.Context) {
	idStr := c.Param("id")
	engineID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的引擎ID"})
		return
	}

	query, language, err := h.sqlEngine.GenerateSampleQuery(c.Request.Context(), uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"query":    query,
		"language": language,
	})
}

// TestConnection 测试数据源连接
// @Summary 测试数据源连接 | Test data source connection
// @Tags Query
// @Accept json
// @Produce json
// @Param id path int true "资源ID | Resource ID"
// @Success 200 {object} map[string]string "连接测试成功 | Connection test successful"
// @Router /test/{id} [get]
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
// @Summary 执行查询语句 | Execute query statement
// @Tags Query
// @Accept json
// @Produce json
// @Param body body ExecuteQueryRequest true "查询请求 | Query request"
// @Success 200 {object} ExecuteQueryResponse "查询结果 | Query result"
// @Router /execute [post]
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

	// 设置默认超时
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 // 默认 30 秒
	}

	// 获取引擎信息，判断是否为 NoSQL 原生查询引擎（MongoDB/Neo4j）
	resource, err := h.sqlEngine.GetEngine(req.EngineID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取引擎配置失败",
			"details": err.Error(),
		})
		return
	}

	// NoSQL 引擎：所有操作统一走 ExecuteSQL（内部路由到原生驱动），不做 SELECT/DML 区分
	if dbbridge.SupportsDirectQuery(resource.EngineType) {
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
			GraphData:    result.GraphData,
		})
		return
	}

	// SQL 引擎：区分查询（SELECT/SHOW/DESC/EXPLAIN）和 DML（INSERT/UPDATE/DELETE）
	sqlLower := strings.ToLower(sql)
	isQuery := strings.HasPrefix(sqlLower, "select") ||
		strings.HasPrefix(sqlLower, "show") ||
		strings.HasPrefix(sqlLower, "desc") ||
		strings.HasPrefix(sqlLower, "explain")

	if isQuery {
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

package api

import (
	"encoding/json"
	"fmt"
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
	sqlEngine     *service.SQLEngineService
	duckdbService *service.DuckDBService
}

// NewQueryHandler 创建 查询处理器
func NewQueryHandler(sqlEngine *service.SQLEngineService, duckdbService *service.DuckDBService) *QueryHandler {
	return &QueryHandler{
		sqlEngine:     sqlEngine,
		duckdbService: duckdbService,
	}
}

// TestConnectionRequest 测试连接请求
type TestConnectionRequest struct {
	EngineID uint `json:"engine_id" binding:"required"`
}

// ExecuteQueryRequest 执行 查询请求
type ExecuteQueryRequest struct {
	Content         map[string]interface{} `json:"content" binding:"required"`
	ExecutionConfig map[string]interface{} `json:"execution_config" binding:"required"`
	Timeout         int                    `json:"timeout"` // 超时时间（秒）
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute"]
// @Router /execute [post]
func (h *QueryHandler) ExecuteQuery(c *gin.Context) {
	var req ExecuteQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sql, err := queryRequestSQL(req.Content)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if sql == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "查询语句不能为空"})
		return
	}

	// 设置默认超时
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 // 默认 30 秒
	}

	queryMode := queryRequestMode(req.ExecutionConfig)
	if queryMode == "duckdb" {
		if _, ok := req.ExecutionConfig["engine_id"]; ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "DuckDB 联邦查询不得提供 execution_config.engine_id"})
			return
		}
		if h.duckdbService == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "DuckDB 联邦查询服务未初始化"})
			return
		}
		result, err := h.duckdbService.ExecuteQuery(c.Request.Context(), tenantIDValue(c), sql, timeout)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, ExecuteQueryResponse{
			Columns:         result.Columns,
			Rows:            result.Rows,
			RowsCount:       result.RowCount,
			RowsAffected:    int64(result.RowCount),
			ExecutionTimeMs: result.ExecutionTimeMs,
		})
		return
	}
	if queryMode != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("不支持的查询执行模式: %s", queryMode)})
		return
	}

	engineID, err := queryRequestEngineID(req.ExecutionConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取引擎信息，判断是否为 NoSQL 原生查询引擎（MongoDB/Neo4j）
	resource, err := h.sqlEngine.GetEngine(engineID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取引擎配置失败",
			"details": err.Error(),
		})
		return
	}

	// NoSQL 引擎：所有操作统一走 ExecuteSQL（内部路由到原生驱动），不做 SELECT/DML 区分
	if dbbridge.SupportsDirectQuery(resource.EngineType) {
		result, err := h.sqlEngine.ExecuteSQL(c.Request.Context(), engineID, sql, timeout)
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
		result, err := h.sqlEngine.ExecuteSQL(c.Request.Context(), engineID, sql, timeout)
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
		rowsAffected, err := h.sqlEngine.ExecuteDML(c.Request.Context(), engineID, sql, timeout)
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

func queryRequestSQL(content map[string]interface{}) (string, error) {
	if content == nil {
		return "", fmt.Errorf("content 不能为空")
	}
	if queryType, ok := content["query_type"].(string); ok && strings.TrimSpace(queryType) != "" && strings.ToLower(strings.TrimSpace(queryType)) != "sql" {
		return "", fmt.Errorf("不支持的查询类型: %s", queryType)
	}
	query, ok := content["query"].(string)
	if !ok {
		return "", fmt.Errorf("content.query 必须提供查询内容")
	}
	return strings.TrimSpace(query), nil
}

func queryRequestMode(executionConfig map[string]interface{}) string {
	if executionConfig == nil {
		return ""
	}
	if mode, ok := executionConfig["query_mode"].(string); ok {
		return strings.ToLower(strings.TrimSpace(mode))
	}
	return ""
}

func queryRequestEngineID(executionConfig map[string]interface{}) (uint, error) {
	if executionConfig == nil {
		return 0, fmt.Errorf("execution_config 不能为空")
	}
	switch value := executionConfig["engine_id"].(type) {
	case float64:
		if value <= 0 {
			return 0, fmt.Errorf("普通查询必须提供 execution_config.engine_id")
		}
		return uint(value), nil
	case int:
		if value <= 0 {
			return 0, fmt.Errorf("普通查询必须提供 execution_config.engine_id")
		}
		return uint(value), nil
	case uint:
		if value == 0 {
			return 0, fmt.Errorf("普通查询必须提供 execution_config.engine_id")
		}
		return value, nil
	case json.Number:
		parsed, err := strconv.ParseUint(string(value), 10, 32)
		if err != nil || parsed == 0 {
			return 0, fmt.Errorf("普通查询必须提供 execution_config.engine_id")
		}
		return uint(parsed), nil
	default:
		return 0, fmt.Errorf("普通查询必须提供 execution_config.engine_id")
	}
}

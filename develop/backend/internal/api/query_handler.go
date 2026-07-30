package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// QueryHandler 查询开发 API 处理器
type QueryHandler struct {
	sqlEngine *service.SQLEngineService
}

// NewQueryHandler 创建 查询处理器
func NewQueryHandler(sqlEngine *service.SQLEngineService) *QueryHandler {
	return &QueryHandler{sqlEngine: sqlEngine}
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
	Columns         []string                   `json:"columns"`
	Rows            []map[string]interface{}   `json:"rows"`
	RowsCount       int                        `json:"rows_count"`
	RowsAffected    int64                      `json:"rows_affected"`
	ExecutionTimeMs int64                      `json:"execution_time_ms"`
	ExecutionID     string                     `json:"execution_id"`
	Effect          service.SQLExecutionEffect `json:"effect"`
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

	query, language, err := h.sqlEngine.GenerateSampleQuery(c.Request.Context(), tenantIDValue(c), uint(engineID))
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
// @Failure 400 {object} map[string]interface{} "资源ID无效 | Invalid resource ID"
// @Failure 401 {object} map[string]interface{} "需要登录 | Authentication required"
// @Failure 403 {object} map[string]interface{} "无执行或数据读取权限 | Execution or data-read permission denied"
// @Failure 409 {object} map[string]interface{} "授权状态冲突 | Authorization conflict"
// @Failure 502 {object} map[string]interface{} "数据源连接失败 | Data source connection failed"
// @Failure 503 {object} map[string]interface{} "授权服务不可用 | Authorization service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute","develop.data_read.execute"]
// @Router /test/{id} [get]
func (h *QueryHandler) TestConnection(c *gin.Context) {
	idStr := c.Param("id")
	engineID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	userAccessToken, err := requestUserAccessToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":      commoni18n.T(c, developi18n.MsgAuthenticationRequired),
			"error_code": "authentication_required",
		})
		return
	}
	if err := h.sqlEngine.TestAuthorizedConnection(
		c.Request.Context(), tenantIDValue(c), userAccessToken, uuid.New(), uint(engineID),
	); err != nil {
		h.writeConnectionTestError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": commoni18n.T(c, developi18n.MsgConnectionTestSuccess),
	})
}

func (h *QueryHandler) writeConnectionTestError(c *gin.Context, err error) {
	if h.writeExecutionAuthorizationError(c, err) {
		return
	}
	status := http.StatusInternalServerError
	if errors.Is(err, service.ErrSQLConnectionTestFailed) {
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{
		"error":      commoni18n.T(c, developi18n.MsgConnectionTestFailed),
		"error_code": "connection_test_failed",
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
// @x-addp-conditional-permissions ["develop.data_read.execute","develop.data_write.execute","develop.data_ddl.execute","develop.data_external_effect.execute"]
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
		c.JSON(http.StatusBadRequest, gin.H{
			"error":      commoni18n.T(c, developi18n.MsgControlledDuckDBUnavailable),
			"error_code": "controlled_duckdb_unavailable",
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

	userAccessToken, err := requestUserAccessToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":      commoni18n.T(c, developi18n.MsgAuthenticationRequired),
			"error_code": "authentication_required",
		})
		return
	}
	executionID := uuid.New()
	result, err := h.sqlEngine.ExecuteAuthorizedSQL(
		c.Request.Context(), tenantIDValue(c), userAccessToken, executionID, engineID, sql, timeout,
	)
	if err != nil {
		h.writeSQLExecutionError(c, err)
		return
	}
	c.JSON(http.StatusOK, ExecuteQueryResponse{
		Columns: result.Columns, Rows: result.Rows, RowsCount: len(result.Rows),
		RowsAffected: result.RowsAffected, ExecutionID: executionID.String(), Effect: result.Effect,
	})
}

func (h *QueryHandler) writeSQLExecutionError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrControlledSQLExecutionUnsupported) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":      commoni18n.T(c, developi18n.MsgControlledSQLEngineUnsupported),
			"error_code": "controlled_sql_engine_unsupported",
		})
		return
	}
	if h.writeExecutionAuthorizationError(c, err) {
		return
	}
	if errors.Is(err, service.ErrSQLExecutionUnclassifiable) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":      commoni18n.T(c, developi18n.MsgSQLClassificationFailed),
			"error_code": "sql_effect_unclassifiable", "details": err.Error(),
		})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{
		"error":      commoni18n.T(c, developi18n.MsgSQLExecutionFailed),
		"error_code": "sql_execution_failed",
	})
}

func (h *QueryHandler) writeExecutionAuthorizationError(c *gin.Context, err error) bool {
	if status, ok := commonClient.SystemAPIStatusCode(err); ok {
		switch status {
		case http.StatusUnauthorized:
			c.JSON(status, gin.H{"error": commoni18n.T(c, developi18n.MsgAuthenticationRequired), "error_code": "authentication_required"})
		case http.StatusForbidden:
			c.JSON(status, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionEffectForbidden), "error_code": "execution_effect_permission_denied"})
		case http.StatusConflict:
			c.JSON(status, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionConflict), "error_code": "execution_authorization_conflict"})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionAuthorizationUnavailable), "error_code": "execution_authorization_unavailable"})
		}
		return true
	}
	return false
}

func requestUserAccessToken(c *gin.Context) (string, error) {
	values := c.Request.Header.Values("Authorization")
	if len(values) != 1 {
		return "", fmt.Errorf("Authorization header must be singular")
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], "addp_at_") || len(parts[1]) == len("addp_at_") {
		return "", fmt.Errorf("Authorization header must contain a canonical Bearer token")
	}
	return parts[1], nil
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

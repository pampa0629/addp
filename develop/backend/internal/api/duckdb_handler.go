package api

import (
	"net/http"

	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// DuckDBHandler DuckDB 联邦查询 API 处理器
type DuckDBHandler struct {
	svc *service.DuckDBService
}

// NewDuckDBHandler 创建 DuckDB 处理器
func NewDuckDBHandler(svc *service.DuckDBService) *DuckDBHandler {
	return &DuckDBHandler{svc: svc}
}

// TestConnection 测试 DuckDB 引擎可用性
// @Summary 测试 DuckDB 连接
// @Description 执行 SELECT 1 验证 DuckDB 引擎可用
// @Tags DuckDB
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /duckdb/test [get]
func (h *DuckDBHandler) TestConnection(c *gin.Context) {
	ms, err := h.svc.Ping(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "DuckDB 引擎不可用: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":           true,
		"message":           "DuckDB 联邦查询引擎运行正常",
		"execution_time_ms": ms,
	})
}

// GetSampleQuery 获取 DuckDB 可执行样例查询
// @Summary 获取 DuckDB 样例查询
// @Tags DuckDB
// @Produce json
// @Success 200 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /duckdb/sample-query [get]
func (h *DuckDBHandler) GetSampleQuery(c *gin.Context) {
	query := h.svc.GenerateSampleQuery(c.Request.Context(), tenantIDValue(c))
	c.JSON(http.StatusOK, gin.H{
		"query":    query,
		"language": "sql",
	})
}

// GetFederatedSources 获取可查询的数据源列表
// @Summary 获取联邦查询数据源
// @Description 返回当前租户下所有可通过 DuckDB 查询的数据源（对象存储表 + 关系型表）
// @Tags DuckDB
// @Produce json
// @Success 200 {array} service.DataSource
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /duckdb/sources [get]
func (h *DuckDBHandler) GetFederatedSources(c *gin.Context) {
	sources, err := h.svc.GetSources(c.Request.Context(), tenantIDValue(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, sources)
}

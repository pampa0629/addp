package api

import (
	"net/http"

	commonClient "github.com/addp/common/client"
	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/gin-gonic/gin"
)

// EngineHandler 引擎管理 API 处理器
type EngineHandler struct {
	systemClient *commonClient.SystemClient
}

// QueryMode 描述 Develop 自有的查询执行模式；它不是 System Engine。
type QueryMode struct {
	Mode        string `json:"mode"`
	Name        string `json:"name"`
	Description string `json:"description"`
	QueryType   string `json:"query_type"`
}

// NewEngineHandler 创建引擎处理器
func NewEngineHandler(systemClient *commonClient.SystemClient) *EngineHandler {
	return &EngineHandler{
		systemClient: systemClient,
	}
}

// ListEngines 获取数据源列表（供 SQL 编辑器使用）
// @Summary 获取可用于 SQL 查询的数据源列表 | List data sources available for SQL queries
// @Tags Engines
// @Produce json
// @Success 200 {array} models.Engine "引擎列表 | Engine list"
// @Router /engines [get]
func (h *EngineHandler) ListEngines(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 从 System 模块获取所有支持 SQL 查询的引擎
	engines, err := h.systemClient.ListSQLQueryEngines(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgEngineListFailed, err.Error()),
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, engines)
}

// ListQueryModes 获取 Develop 内置查询模式列表
// @Summary 获取 Develop 内置查询模式列表 | List Develop-owned query modes
// @Tags Engines
// @Produce json
// @Success 200 {array} QueryMode "查询模式列表 | Query mode list"
// @Router /query-modes [get]
func (h *EngineHandler) ListQueryModes(c *gin.Context) {
	c.JSON(http.StatusOK, []QueryMode{
		{
			Mode:        "duckdb",
			Name:        "DuckDB 联邦查询",
			Description: "Develop 内置联邦查询模式，支持对象存储表与关系型表跨源 JOIN",
			QueryType:   "sql",
		},
	})
}

// ListWorkflowEngines 获取工作流引擎列表
// @Summary 获取支持 workflow 的计算引擎列表 | List workflow-capable compute engines
// @Tags Engines
// @Produce json
// @Success 200 {array} models.Engine "工作流引擎列表 | Workflow engine list"
// @Router /workflow-engines [get]
func (h *EngineHandler) ListWorkflowEngines(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 从 System 模块获取所有工作流引擎
	engines, err := h.systemClient.ListWorkflowEngines(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgWorkflowListFailed, err.Error()),
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, engines)
}

// ListSparkRuntimes 获取 Apache Spark 通用引擎资源列表
// @Summary 获取所有 Apache Spark 通用引擎资源列表 | List all Apache Spark general engine resources
// @Tags Engines
// @Produce json
// @Success 200 {array} models.Engine "Spark通用引擎资源列表 | Spark general engine resource list"
// @Router /spark-runtimes [get]
func (h *EngineHandler) ListSparkRuntimes(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 从 System 模块获取所有 Spark 通用引擎资源
	runtimes, err := h.systemClient.ListSparkRuntimes(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgSparkListFailed, err.Error()),
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, runtimes)
}

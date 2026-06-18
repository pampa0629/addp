package api

import (
	"net/http"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/gin-gonic/gin"
)

// EngineHandler 引擎管理 API 处理器
type EngineHandler struct {
	systemClient *commonClient.SystemClient
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
// @Success 200 {object} map[string]interface{} "引擎列表 | Engine list"
// @Router /engines [get]
func (h *EngineHandler) ListEngines(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 从 System 模块获取所有支持 SQL 查询的引擎
	engines, err := h.systemClient.ListSQLQueryEngines(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取引擎列表失败",
			"details": err.Error(),
		})
		return
	}

	// 追加虚拟 DuckDB 引擎条目（内置联邦查询引擎，不注册到 System）
	duckdbEngine := commonModels.Engine{
		ID:          0, // 虚拟 ID，前端用 engine_type 判断路由
		Name:        "DuckDB 联邦查询",
		EngineType:  "duckdb",
		Description: "内置联邦查询引擎，支持对象存储表与关系型表跨源 JOIN",
		IsBuiltin:   true,
		IsActive:    true,
	}
	engines = append(engines, duckdbEngine)

	// 返回引擎列表（与前端期望的格式一致）
	c.JSON(http.StatusOK, engines)
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
			"error":   "获取工作流引擎失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, engines)
}

// ListSparkRuntimes 获取 Apache Spark 运行时列表
// @Summary 获取所有 Apache Spark 运行时列表 | List all Apache Spark runtimes
// @Tags Engines
// @Produce json
// @Success 200 {array} models.Engine "Spark运行时列表 | Spark runtime list"
// @Router /spark-runtimes [get]
func (h *EngineHandler) ListSparkRuntimes(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 从 System 模块获取所有 Spark 运行时
	runtimes, err := h.systemClient.ListSparkRuntimes(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取 Spark 运行时失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, runtimes)
}

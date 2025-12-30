package api

import (
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
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
// @Summary 获取可用于 SQL 查询的数据源列表
// @Tags Engines
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/engines [get]
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

	// 返回引擎列表（与前端期望的格式一致）
	c.JSON(http.StatusOK, engines)
}

// ListSchemas 获取指定引擎的 schema 列表
// @Summary 获取数据库 schema 列表
// @Tags Engines
// @Produce json
// @Param id path int true "引擎ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/engines/:id/schemas [get]
func (h *EngineHandler) ListSchemas(c *gin.Context) {
	engineIDStr := c.Param("id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的引擎ID"})
		return
	}

	// 使用 SystemClient 获取 schema 列表
	schemas, err := h.systemClient.ListSchemas(uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取 schema 列表失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"schemas": schemas,
	})
}

// ListTables 获取指定引擎的表列表
// @Summary 获取数据库表列表
// @Tags Engines
// @Produce json
// @Param id path int true "引擎ID"
// @Param schema query string false "Schema名称"
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/engines/:id/tables [get]
func (h *EngineHandler) ListTables(c *gin.Context) {
	engineIDStr := c.Param("id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的引擎ID"})
		return
	}

	schema := c.Query("schema")
	if schema == "" {
		schema = "public" // 默认 schema
	}

	// 使用 SystemClient 获取表列表
	tables, err := h.systemClient.ListTables(uint(engineID), schema)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取表列表失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"tables": tables,
	})
}

// ListWorkflowEngines 获取工作流引擎列表
// @Summary 获取支持 workflow 的计算引擎列表
// @Tags Engines
// @Produce json
// @Success 200 {array} models.Engine
// @Router /api/develop/workflow-engines [get]
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
// @Summary 获取所有 Apache Spark 运行时列表
// @Tags Engines
// @Produce json
// @Success 200 {array} models.Engine
// @Router /api/develop/spark-runtimes [get]
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

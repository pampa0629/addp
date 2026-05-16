package api

import (
	"net/http"
	"strconv"
	"strings"

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
		Description: "内置联邦查询引擎，支持湖表（Parquet）与关系型表跨源 JOIN",
		IsBuiltin:   true,
		IsActive:    true,
	}
	engines = append(engines, duckdbEngine)

	// 返回引擎列表（与前端期望的格式一致）
	c.JSON(http.StatusOK, engines)
}

// ListNfsEngines 获取 NFS 引擎列表（供工作流 NFS 文件选择器使用）
// @Summary 获取 NFS 存储引擎列表 | List NFS storage engines
// @Tags Engines
// @Produce json
// @Success 200 {array} commonModels.Engine "NFS 引擎列表"
// @Router /engines/nfs [get]
func (h *EngineHandler) ListNfsEngines(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	engines, err := h.systemClient.ListEngines("nfs", tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取NFS引擎列表失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, engines)
}

// ListCatalogChildren 获取指定引擎的实时 catalog 子节点。
// @Summary 获取实时 catalog 子节点 | List live catalog children
// @Tags Engines
// @Accept json
// @Produce json
// @Param id path int true "引擎ID | Engine ID"
// @Param request body commonClient.EngineCatalogListChildrenRequest true "Catalog 路径请求 | Catalog path request"
// @Success 200 {object} commonClient.EngineCatalogListChildrenResponse "Catalog 子节点 | Catalog children"
// @Router /engines/{id}/catalog/children [post]
func (h *EngineHandler) ListCatalogChildren(c *gin.Context) {
	engineIDStr := c.Param("id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的引擎ID"})
		return
	}

	var req commonClient.EngineCatalogListChildrenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	nodes, err := h.systemClient.ListCatalogChildrenWithToken(uint(engineID), req, bearerToken(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取 catalog 子节点失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, commonClient.EngineCatalogListChildrenResponse{Nodes: nodes})
}

func bearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
		return authHeader[7:]
	}
	return ""
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

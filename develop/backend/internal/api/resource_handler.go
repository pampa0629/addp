package api

import (
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

// ResourceHandler 资源管理 API 处理器
type ResourceHandler struct {
	systemClient *commonClient.SystemClient
}

// NewResourceHandler 创建资源处理器
func NewResourceHandler(systemClient *commonClient.SystemClient) *ResourceHandler {
	return &ResourceHandler{
		systemClient: systemClient,
	}
}

// ListResources 获取数据源列表（供 SQL 编辑器使用）
// @Summary 获取可用于 SQL 查询的数据源列表
// @Tags Resources
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/resources [get]
func (h *ResourceHandler) ListResources(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 从 System 模块获取所有支持 SQL 查询的资源
	resources, err := h.systemClient.ListSQLQueryEngines(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取资源列表失败",
			"details": err.Error(),
		})
		return
	}

	// 返回资源列表（与前端期望的格式一致）
	c.JSON(http.StatusOK, resources)
}

// ListSchemas 获取指定资源的 schema 列表
// @Summary 获取数据库 schema 列表
// @Tags Resources
// @Produce json
// @Param id path int true "资源ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/resources/:id/schemas [get]
func (h *ResourceHandler) ListSchemas(c *gin.Context) {
	resourceIDStr := c.Param("id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	// 使用 SystemClient 获取 schema 列表
	schemas, err := h.systemClient.ListSchemas(uint(resourceID))
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

// ListTables 获取指定资源的表列表
// @Summary 获取数据库表列表
// @Tags Resources
// @Produce json
// @Param id path int true "资源ID"
// @Param schema query string false "Schema名称"
// @Success 200 {object} map[string]interface{}
// @Router /api/develop/resources/:id/tables [get]
func (h *ResourceHandler) ListTables(c *gin.Context) {
	resourceIDStr := c.Param("id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	schema := c.Query("schema")
	if schema == "" {
		schema = "public" // 默认 schema
	}

	// 使用 SystemClient 获取表列表
	tables, err := h.systemClient.ListTables(uint(resourceID), schema)
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
// @Tags Resources
// @Produce json
// @Success 200 {array} models.Resource
// @Router /api/develop/workflow-engines [get]
func (h *ResourceHandler) ListWorkflowEngines(c *gin.Context) {
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

// ListSparkRuntimes 获取 Spark 运行时列表
// @Summary 获取所有 Spark SQL 运行时列表
// @Tags Resources
// @Produce json
// @Success 200 {array} models.Resource
// @Router /api/develop/spark-runtimes [get]
func (h *ResourceHandler) ListSparkRuntimes(c *gin.Context) {
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

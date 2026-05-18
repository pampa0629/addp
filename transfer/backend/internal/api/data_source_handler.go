package api

import (
	"net/http"
	"strconv"

	"github.com/addp/common/catalogview"
	commonClient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

// DataSourceHandler 数据源处理器
// 为 Transfer 前端提供统一的数据源访问接口
// 调用 common 的 DataSourceService 实现核心逻辑
type DataSourceHandler struct {
	dataSourceService *catalogview.DataSourceService
}

// NewDataSourceHandler 创建数据源处理器
func NewDataSourceHandler(systemClient catalogview.SystemClient, metaClient catalogview.MetaClient) *DataSourceHandler {
	return &DataSourceHandler{
		dataSourceService: catalogview.NewDataSourceService(systemClient, metaClient),
	}
}

// GetEngines 获取存储引擎列表
// @Summary 获取存储引擎列表 | List engines
// @Tags 数据源 | Data Sources
// @Produce json
// @Param engine_types query string false "引擎类型列表 | Engine types"
// @Param data_source_types query string false "数据源类型列表 | Data source types"
// @Success 200 {array} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /engines [get]
// @Security BearerAuth
func (h *DataSourceHandler) GetEngines(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id in token"})
		return
	}

	// 解析过滤条件
	var filters catalogview.EngineFilters

	// 引擎类型过滤
	if engineTypesStr := c.Query("engine_types"); engineTypesStr != "" {
		// 逗号分隔的引擎类型列表
		filters.EngineTypes = parseCommaSeparated(engineTypesStr)
	}

	// 数据源类型过滤
	if dataSourceTypesStr := c.Query("data_source_types"); dataSourceTypesStr != "" {
		filters.DataSourceTypes = parseCommaSeparated(dataSourceTypesStr)
	}

	// 调用 DataSourceService
	engines, err := h.dataSourceService.GetEngines(tenantID, filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engines: " + err.Error()})
		return
	}

	// 直接返回数组（符合 ADDP API 规范）
	c.JSON(http.StatusOK, engines)
}

// GetEngineTree 获取引擎的元数据树
// @Summary 获取引擎元数据树 | Get engine metadata tree
// @Tags 数据源 | Data Sources
// @Produce json
// @Param engine_id path int true "引擎ID | Engine ID"
// @Param expand_depth query int false "展开深度 | Expand depth" default(1)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /engines/{engine_id}/tree [get]
// @Security BearerAuth
func (h *DataSourceHandler) GetEngineTree(c *gin.Context) {
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid engine_id"})
		return
	}

	tenantID := c.GetUint("tenant_id")
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id in token"})
		return
	}

	// 解析展开深度
	expandDepth := 1 // 默认展开一层
	if expandDepthStr := c.Query("expand_depth"); expandDepthStr != "" {
		if depth, err := strconv.Atoi(expandDepthStr); err == nil {
			expandDepth = depth
		}
	}

	// 调用 DataSourceService
	tree, err := h.dataSourceService.GetEngineTree(uint(engineID), expandDepth)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engine tree: " + err.Error()})
		return
	}

	// 直接返回对象
	c.JSON(http.StatusOK, tree)
}

// GetNodeChildren 获取节点的子节点（懒加载）
// @Summary 获取节点子节点 | Get node children
// @Tags 数据源 | Data Sources
// @Produce json
// @Param node_id path int true "节点ID | Node ID"
// @Success 200 {array} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /nodes/{node_id}/children [get]
// @Security BearerAuth
func (h *DataSourceHandler) GetNodeChildren(c *gin.Context) {
	nodeIDStr := c.Param("node_id")
	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid node_id"})
		return
	}

	// 调用 DataSourceService
	children, err := h.dataSourceService.GetNodeChildren(uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get node children: " + err.Error()})
		return
	}

	// 直接返回数组
	c.JSON(http.StatusOK, children)
}

// DetectTableMetadata 检测表元数据（几何列检测）
// @Summary 检测表元数据 | Detect table metadata
// @Tags 数据源 | Data Sources
// @Produce json
// @Param engine_id query int true "引擎ID | Engine ID"
// @Param schema query string true "Schema"
// @Param table query string true "表名 | Table"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tables/metadata [get]
// @Security BearerAuth
func (h *DataSourceHandler) DetectTableMetadata(c *gin.Context) {
	engineIDStr := c.Query("engine_id")
	if engineIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing engine_id parameter"})
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid engine_id"})
		return
	}

	schema := c.Query("schema")
	if schema == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing schema parameter"})
		return
	}

	table := c.Query("table")
	if table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing table parameter"})
		return
	}

	// 调用 DataSourceService
	metadata, err := h.dataSourceService.DetectTableMetadata(uint(engineID), schema, table)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to detect table metadata: " + err.Error()})
		return
	}

	// 直接返回对象
	c.JSON(http.StatusOK, metadata)
}

// 辅助函数

// parseCommaSeparated 解析逗号分隔的字符串
func parseCommaSeparated(s string) []string {
	if s == "" {
		return []string{}
	}

	result := []string{}
	for _, part := range splitAndTrim(s, ",") {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// splitAndTrim 分割并去除空格
func splitAndTrim(s, sep string) []string {
	parts := []string{}
	for _, part := range splitString(s, sep) {
		trimmed := trimString(part)
		parts = append(parts, trimmed)
	}
	return parts
}

// splitString 简单字符串分割（避免使用 strings.Split）
func splitString(s, sep string) []string {
	if s == "" {
		return []string{}
	}

	result := []string{}
	start := 0

	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}

	// 添加最后一段
	result = append(result, s[start:])
	return result
}

// trimString 去除字符串两端空格
func trimString(s string) string {
	start := 0
	end := len(s)

	// 去除前导空格
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	// 去除尾随空格
	for start < end && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}

// NewDataSourceHandlerWithClients 使用已有的客户端创建处理器
// 用于兼容现有的 Transfer 模块结构
func NewDataSourceHandlerWithClients(systemClient *commonClient.SystemClient, metaURL, authToken string) *DataSourceHandler {
	// 创建 MetaClient
	metaClient := commonClient.NewMetaClient(metaURL, authToken)

	// 使用 DataSourceService
	return &DataSourceHandler{
		dataSourceService: catalogview.NewDataSourceService(systemClient, metaClient),
	}
}

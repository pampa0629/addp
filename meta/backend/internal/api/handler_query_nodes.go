package api

import (
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
)

var (
	_ = models.MetadataTreeResponse{}
	_ = models.MetaNodeLite{}
)

// GetMetadataTree 获取存储引擎的完整元数据树
// @Summary 获取元数据树 | Get metadata tree
// @Description 获取指定引擎的完整元数据树 | Get metadata tree for an engine
// @Tags Meta Query
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Success 200 {object} models.MetadataTreeResponse "元数据树 | Metadata tree"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines/{engine_id}/tree [get]
// @Security BearerAuth
func (h *Handler) GetMetadataTree(c *gin.Context) {
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}
	tenantID, err := h.effectiveTenantIDForEngine(c, uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tree, err := h.metadataQueryService.GetMetadataTree(tenantID, uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tree)
}

// GetMetaNodeByID 获取单个节点详情
// @Summary 获取节点详情 | Get node detail
// @Description 按节点 ID 获取元数据节点详情 | Get metadata node detail by ID
// @Tags Meta Query
// @Produce json
// @Param node_id path int true "节点ID | Node ID"
// @Success 200 {object} models.MetaNodeLite "节点详情 | Node detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "节点不存在 | Node not found"
// @Router /nodes/{node_id} [get]
// @Security BearerAuth
func (h *Handler) GetMetaNodeByID(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	nodeIDStr := c.Param("node_id")
	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node_id"})
		return
	}

	node, err := h.metadataQueryService.GetMetaNodeByID(tenantID, uint(nodeID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, node)
}

// GetNodeAncestors 获取节点祖先链
// @Summary 获取节点祖先链 | Get node ancestor chain
// @Description 按节点 ID 获取 root 到目标节点的元数据节点链，包含目标节点自身 | Get metadata node chain from root to target node, including the target node
// @Tags Meta Query
// @Produce json
// @Param node_id path int true "节点ID | Node ID"
// @Success 200 {array} models.MetaNodeLite "节点祖先链 | Node ancestor chain"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "祖先链不存在 | Ancestor chain not found"
// @Router /nodes/{node_id}/ancestors [get]
// @Security BearerAuth
func (h *Handler) GetNodeAncestors(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	nodeIDStr := c.Param("node_id")
	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node_id"})
		return
	}

	ancestors, err := h.metadataQueryService.GetNodeAncestors(tenantID, uint(nodeID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ancestors)
}

// GetNodeChildren 获取节点的子节点
// @Summary 获取子节点 | Get node children
// @Description 获取指定节点的直接子节点 | Get direct children of a metadata node
// @Tags Meta Query
// @Produce json
// @Param node_id path int true "节点ID | Node ID"
// @Success 200 {array} models.MetaNodeLite "子节点列表 | Children"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /nodes/{node_id}/children [get]
// @Security BearerAuth
func (h *Handler) GetNodeChildren(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	nodeIDStr := c.Param("node_id")
	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node_id"})
		return
	}

	nodes, err := h.metadataQueryService.GetNodeChildren(tenantID, uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, nodes)
}

// QueryNodeByCatalogPath 按 catalog path 查询节点
// @Summary 按 catalog path 查询节点 | Query node by catalog path
// @Description 按引擎和 catalog path 查询元数据节点 | Query metadata node by engine and catalog path
// @Tags Meta Query
// @Produce json
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param catalog_path query string true "Catalog path"
// @Success 200 {object} models.MetaNodeLite "节点详情 | Node detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "节点不存在 | Node not found"
// @Router /nodes/by-catalog-path [get]
// @Security BearerAuth
func (h *Handler) QueryNodeByCatalogPath(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	catalogPath, ok := c.GetQuery("catalog_path")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing catalog_path parameter"})
		return
	}

	node, err := h.metadataQueryService.GetNodeByCatalogPath(tenantID, uint(engineID), catalogPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, node)
}

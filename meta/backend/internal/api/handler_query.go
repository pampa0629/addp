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
	_ = models.MetaItemLite{}
	_ = models.SpatialMetadataResponse{}
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

// GetNodeItems 获取节点下的数据项
// @Summary 获取节点数据项 | Get node items
// @Description 获取指定节点下的数据项 | Get metadata items under a node
// @Tags Meta Query
// @Produce json
// @Param node_id path int true "节点ID | Node ID"
// @Success 200 {array} models.MetaItemLite "数据项列表 | Items"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /nodes/{node_id}/items [get]
// @Security BearerAuth
func (h *Handler) GetNodeItems(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	nodeIDStr := c.Param("node_id")
	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node_id"})
		return
	}

	items, err := h.metadataQueryService.GetNodeItems(tenantID, uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, items)
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

// QueryItemByCatalogPath 按 catalog path 查询数据项
// @Summary 按 catalog path 查询数据项 | Query item by catalog path
// @Description 按引擎和 catalog path 查询数据项 | Query metadata item by engine and catalog path
// @Tags Meta Query
// @Produce json
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param catalog_path query string true "Catalog path"
// @Success 200 {object} models.MetaItemLite "数据项详情 | Item detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "数据项不存在 | Item not found"
// @Router /items/by-catalog-path [get]
// @Security BearerAuth
func (h *Handler) QueryItemByCatalogPath(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	catalogPath := c.Query("catalog_path")
	if catalogPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing catalog_path parameter"})
		return
	}

	item, err := h.metadataQueryService.GetItemByCatalogPath(tenantID, uint(engineID), catalogPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetItemByID 按 ID 查询 MetaItem
// @Summary 获取数据项详情 | Get item detail
// @Description 按数据项 ID 获取元数据项详情 | Get metadata item detail by ID
// @Tags Meta Query
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Success 200 {object} models.MetaItemLite "数据项详情 | Item detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "数据项不存在 | Item not found"
// @Router /items/{item_id} [get]
// @Security BearerAuth
func (h *Handler) GetItemByID(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	itemIDStr := c.Param("item_id")
	itemID, err := strconv.ParseUint(itemIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item_id"})
		return
	}

	item, err := h.metadataQueryService.GetItemByID(tenantID, uint(itemID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetItemSpatialMetadataByID 按 item_id 获取数据项空间元数据。
// @Summary 按 ID 获取空间元数据 | Get spatial metadata by ID
// @Description 按数据项 ID 获取空间元数据 | Get spatial metadata by item ID
// @Tags Meta Query
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Success 200 {object} models.SpatialMetadataResponse "空间元数据 | Spatial metadata"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "空间元数据不存在 | Spatial metadata not found"
// @Router /items/{item_id}/spatial [get]
// @Security BearerAuth
func (h *Handler) GetItemSpatialMetadataByID(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	itemIDStr := c.Param("item_id")
	itemID, err := strconv.ParseUint(itemIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item_id"})
		return
	}

	spatialMeta, err := h.metadataQueryService.GetItemSpatialMetadataByID(tenantID, uint(itemID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, spatialMeta)
}

package api

import (
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
)

var _ = models.MetaItemLite{}

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

// GetItemAncestors 获取数据项祖先链
// @Summary 获取数据项祖先链 | Get item ancestor chain
// @Description 按数据项 ID 获取数据项及 root 到其父节点的元数据节点链 | Get item and metadata node chain from root to its parent node by item ID
// @Tags Meta Query
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Success 200 {object} models.MetaItemAncestorsResponse "数据项祖先链 | Item ancestor chain"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "祖先链不存在 | Ancestor chain not found"
// @Router /items/{item_id}/ancestors [get]
// @Security BearerAuth
func (h *Handler) GetItemAncestors(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	itemIDStr := c.Param("item_id")
	itemID, err := strconv.ParseUint(itemIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item_id"})
		return
	}

	result, err := h.metadataQueryService.GetItemAncestors(tenantID, uint(itemID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

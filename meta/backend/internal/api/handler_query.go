package api

import (
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
)

var (
	_ = models.MetadataTreeResponse{}
	_ = models.MetaItemAncestorsResponse{}
	_ = models.MetaNodeLite{}
	_ = models.MetaItemLite{}
	_ = models.SpatialMetadataResponse{}
)

// GetItemSpatialMetadataByID 按 item_id 获取数据项空间元数据。
// @Summary 按 ID 获取空间元数据 | Get spatial metadata by ID
// @Description 按数据项 ID 获取空间元数据 | Get spatial metadata by item ID
// @Description PG 用户查询按当前 metadata_read/read 权限过滤，仅返回身份与结构，不含样本、源数据统计或原始 attributes；不可见单项与不存在均返回 404；受控机器事实查询保留既有契约 | PG user queries enforce current metadata_read/read visibility and return identity and structure only, without samples, source statistics or raw attributes; hidden and absent single targets both return 404; guarded machine facts retain their contract
// @Tags Meta Query
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Success 200 {object} models.SpatialMetadataResponse "空间元数据 | Spatial metadata"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "空间元数据不存在 | Spatial metadata not found"
// @Failure 401 {object} map[string]interface{} "认证失效 | Invalid credential"
// @Failure 403 {object} map[string]interface{} "无查询权限 | Query forbidden"
// @Failure 503 {object} map[string]interface{} "授权核验不可用 | Authorization check unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.catalog.read"]
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

	item, err := h.metadataQueryService.GetItemByID(tenantID, uint(itemID))
	if err != nil {
		h.respondMetadataQueryError(c, err)
		return
	}
	item, err = h.visibleMetadataItem(c, item)
	if err != nil {
		h.respondMetadataQueryError(c, err)
		return
	}
	spatialMeta, err := metaquery.SpatialMetadataFromItem(models.MetaItem{Attributes: item.Attributes, RowCount: item.RowCount})
	if err != nil {
		h.respondMetadataQueryError(c, err)
		return
	}

	c.JSON(http.StatusOK, spatialMeta)
}

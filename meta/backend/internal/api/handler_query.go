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
	_ = models.MetaItemAncestorsResponse{}
	_ = models.MetaNodeLite{}
	_ = models.MetaItemLite{}
	_ = models.SpatialMetadataResponse{}
)

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

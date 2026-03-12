package api

import (
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	developModels "github.com/addp/develop/backend/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type assetDiscoverableHandler struct {
	db *gorm.DB
}

func newAssetDiscoverableHandler(db *gorm.DB) *assetDiscoverableHandler {
	return &assetDiscoverableHandler{db: db}
}

// listDiscoverableAssets GET /api/develop/assets/discoverable
// 返回当前租户下 status='active' 的开发项，供 Asset 模块自动发现。
// source_reference 格式: "{item.ID}"
func (h *assetDiscoverableHandler) listDiscoverableAssets(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	var items []developModels.DevTask
	if err := h.db.Where("tenant_id = ? AND status = 'active'", tenantID).
		Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]commonClient.DiscoverableAsset, 0, len(items))
	for _, item := range items {
		name := item.DisplayName
		if name == "" {
			name = item.Name
		}
		result = append(result, commonClient.DiscoverableAsset{
			SourceReference: strconv.FormatUint(uint64(item.ID), 10),
			Name:            name,
			Description:     item.Description,
		})
	}

	c.JSON(http.StatusOK, result)
}

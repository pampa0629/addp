package api

import (
	"fmt"
	"net/http"

	commonClient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	metaModels "github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type assetDiscoverableHandler struct {
	db *gorm.DB
}

func newAssetDiscoverableHandler(db *gorm.DB) *assetDiscoverableHandler {
	return &assetDiscoverableHandler{db: db}
}

// listDiscoverableAssets GET /api/meta/assets/discoverable
// 返回当前租户下已扫描完成的 MetaItem，供 Asset 模块自动发现注册。
// source_reference 格式: "{engine_id}:{full_name}"
func (h *assetDiscoverableHandler) listDiscoverableAssets(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	var items []metaModels.MetaItem
	if err := h.db.Where("tenant_id = ? AND scanned_at IS NOT NULL", tenantID).
		Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]commonClient.DiscoverableAsset, 0, len(items))
	for _, item := range items {
		result = append(result, commonClient.DiscoverableAsset{
			SourceReference: fmt.Sprintf("%d:%s", item.EngineID, item.FullName),
			Name:            item.Name,
			Description:     "",
		})
	}

	c.JSON(http.StatusOK, result)
}

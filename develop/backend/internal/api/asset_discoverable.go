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

// listDiscoverableAssets 返回当前租户下 status='active' 的开发任务，供 Asset 模块自动发现。
// @Summary 列出可发现资产 | List discoverable assets
// @Description 返回当前租户下可被资产模块发现的开发任务 | List active develop tasks for Asset discovery
// @Tags Develop
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /assets/discoverable [get]
// @Security BearerAuth
func (h *assetDiscoverableHandler) listDiscoverableAssets(c *gin.Context) {
	tenantID := tenantIDValue(c)

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

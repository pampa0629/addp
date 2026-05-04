package api

import (
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	serviceModels "github.com/addp/service/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type assetDiscoverableHandler struct {
	db *gorm.DB
}

func newAssetDiscoverableHandler(db *gorm.DB) *assetDiscoverableHandler {
	return &assetDiscoverableHandler{db: db}
}

// listDiscoverableAssets 返回当前租户下 status='active' 的所有服务（查询服务、注册服务、瓦片服务），供 Asset 模块自动发现。
// @Summary 列出可发现资产 | List discoverable assets
// @Tags Service
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /assets/discoverable [get]
// @Security BearerAuth
func (h *assetDiscoverableHandler) listDiscoverableAssets(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	result := make([]commonClient.DiscoverableAsset, 0)

	// 查询服务
	var queryServices []serviceModels.QueryService
	if err := h.db.Where("tenant_id = ? AND status = 'active'", tenantID).Find(&queryServices).Error; err == nil {
		for _, svc := range queryServices {
			result = append(result, commonClient.DiscoverableAsset{
				SourceReference: "query:" + strconv.FormatUint(uint64(svc.ID), 10),
				Name:            svc.Title,
				Description:     svc.Description,
			})
		}
	}

	// 注册服务
	var registeredServices []serviceModels.RegisteredService
	if err := h.db.Where("tenant_id = ? AND status = 'active'", tenantID).Find(&registeredServices).Error; err == nil {
		for _, svc := range registeredServices {
			result = append(result, commonClient.DiscoverableAsset{
				SourceReference: "registered:" + strconv.FormatUint(uint64(svc.ID), 10),
				Name:            svc.Title,
				Description:     svc.Description,
			})
		}
	}

	// 瓦片服务
	var tileServices []serviceModels.TileService
	if err := h.db.Where("tenant_id = ? AND status = 'active'", tenantID).Find(&tileServices).Error; err == nil {
		for _, svc := range tileServices {
			result = append(result, commonClient.DiscoverableAsset{
				SourceReference: "tile:" + strconv.FormatUint(uint64(svc.ID), 10),
				Name:            svc.Title,
				Description:     svc.Description,
			})
		}
	}

	c.JSON(http.StatusOK, result)
}

package api

import (
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	standardModels "github.com/addp/standard/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type assetDiscoverableHandler struct {
	db *gorm.DB
}

func newAssetDiscoverableHandler(db *gorm.DB) *assetDiscoverableHandler {
	return &assetDiscoverableHandler{db: db}
}

// listDiscoverableAssets 返回当前租户下 status='approved' 的指标，供 Asset 模块自动发现。
// source_reference 格式: "{metric.ID}"
// @Summary 可发现资产列表 | List discoverable assets
// @Tags AssetDiscoverable
// @Produce json
// @Success 200 {array} commonClient.DiscoverableAsset
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /assets/discoverable [get]
// @Security BearerAuth
func (h *assetDiscoverableHandler) listDiscoverableAssets(c *gin.Context) {
	tenantID := getTenantID(c)

	var metrics []standardModels.Metric
	if err := h.db.Where("tenant_id = ? AND status = 'approved'", tenantID).
		Find(&metrics).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]commonClient.DiscoverableAsset, 0, len(metrics))
	for _, m := range metrics {
		result = append(result, commonClient.DiscoverableAsset{
			SourceReference: strconv.FormatInt(m.ID, 10),
			Name:            m.Name,
			Description:     m.Definition,
		})
	}

	c.JSON(http.StatusOK, result)
}

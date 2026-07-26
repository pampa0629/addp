package api

import (
	"errors"

	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func spatialMetadataFromMeta(
	c *gin.Context,
	quickViewService *service.QuickViewService,
	engineID uint,
	schema string,
	table string,
) (*service.SpatialMetadataResult, error) {
	if quickViewService == nil {
		return nil, errors.New("quick view service is not initialized")
	}
	tenantID := tenantIDValue(c)
	if tenantID == 0 {
		return nil, errors.New("tenant_id is required")
	}
	return quickViewService.GetSpatialMetadataFromMeta(c.Request.Context(), tenantID, engineID, schema, table)
}

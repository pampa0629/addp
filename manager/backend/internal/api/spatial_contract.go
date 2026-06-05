package api

import (
	"fmt"

	"github.com/addp/common/spatial"
	"github.com/gin-gonic/gin"
)

func spatialPreviewContract(geometryColumn string, sourceSRID int) gin.H {
	contract := gin.H{
		"geometry_column": geometryColumn,
		"source_srid":     sourceSRID,
		"target_srid":     nil,
	}
	if sourceSRID > 0 {
		contract["source_crs"] = fmt.Sprintf("EPSG:%d", sourceSRID)
		contract["transform_status"] = "not_transformed"
		if sourceSRID == spatial.SRIDWGS84 {
			contract["preview_hint"] = "direct_renderable"
		} else {
			contract["preview_hint"] = "frontend_transform_required"
		}
		return contract
	}

	contract["transform_status"] = "unknown_crs"
	contract["preview_hint"] = "unknown_crs"
	contract["transform_message"] = "源坐标系未知，已跳过地图渲染"
	return contract
}

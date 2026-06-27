package metaattr

import (
	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
)

func Model3DInfoAttributes(modelInfo *datatype.Model3DInfo, spatialInfo *datatype.SpatialInfo) models.JSONMap {
	attrs := models.JSONMap{}
	if modelPayload := datatype.Model3DInfoPayload(modelInfo); len(modelPayload) > 0 {
		UpsertNested(attrs, "type_info", "model_3d", modelPayload)
	}
	if spatialPayload := datatype.SpatialInfoPayload(spatialInfo); len(spatialPayload) > 0 {
		UpsertNested(attrs, "capabilities", "spatial", spatialPayload)
	}
	return attrs
}

func PointCloudInfoAttributes(pointCloudInfo *datatype.PointCloudInfo, spatialInfo *datatype.SpatialInfo) models.JSONMap {
	attrs := models.JSONMap{}
	if pointCloudPayload := datatype.PointCloudInfoPayload(pointCloudInfo); len(pointCloudPayload) > 0 {
		UpsertNested(attrs, "type_info", "point_cloud", pointCloudPayload)
	}
	if spatialPayload := datatype.SpatialInfoPayload(spatialInfo); len(spatialPayload) > 0 {
		UpsertNested(attrs, "capabilities", "spatial", spatialPayload)
	}
	return attrs
}

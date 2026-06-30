package metaitem

import (
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
)

func AttributeInput(item *DetectedItem) metaattr.DataItemAttributesInput {
	if item == nil {
		return metaattr.DataItemAttributesInput{}
	}
	return metaattr.DataItemAttributesInput{
		Attributes:   item.Attributes,
		Layout:       string(item.Layout),
		DataType:     item.DataType,
		Format:       item.Format,
		PhysicalPath: item.PhysicalPath,
		RefList:      refAttributeInput(item.RefList),
		SizeBytes:    item.SizeBytes,
	}
}

func refAttributeInput(refs []dataitem.ItemRef) []metaattr.ItemRefAttributesInput {
	if len(refs) == 0 {
		return nil
	}
	result := make([]metaattr.ItemRefAttributesInput, 0, len(refs))
	for _, ref := range refs {
		result = append(result, metaattr.ItemRefAttributesInput{
			Role:      ref.Role,
			Path:      ref.Path,
			Required:  ref.Required,
			Primary:   ref.Primary,
			Extension: ref.Extension,
		})
	}
	return result
}

func ApplyContainerSummary(attrs models.JSONMap, item *DetectedItem) {
	if attrs == nil || item == nil || item.DataType != datatype.Container {
		return
	}
	if item.Container == nil {
		item.Container = &datatype.ContainerInfo{
			Children:      []datatype.ContainerChildInfo{},
			ChildCount:    0,
			ResourceCount: 1,
		}
	}
	ApplyContainerInfo(attrs, item)
}

func ApplyDocumentInfo(attrs models.JSONMap, item *DetectedItem) {
	if attrs == nil || item == nil || item.Document == nil {
		return
	}
	metaattr.MergeAttributeMaps(attrs, metaattr.DocumentInfoAttributes(item.Document))
}

func ApplyMediaInfo(attrs models.JSONMap, item *DetectedItem, spatialInfo *datatype.SpatialInfo) {
	if attrs == nil || item == nil || item.Media == nil {
		return
	}
	metaattr.MergeAttributeMaps(attrs, metaattr.MediaInfoAttributes(item.Media, spatialInfo))
}

func ApplyModel3DInfo(attrs models.JSONMap, item *DetectedItem, spatialInfo *datatype.SpatialInfo) {
	if attrs == nil || item == nil || item.Model3D == nil {
		return
	}
	metaattr.MergeAttributeMaps(attrs, metaattr.Model3DInfoAttributes(item.Model3D, spatialInfo))
}

func ApplyPointCloudInfo(attrs models.JSONMap, item *DetectedItem, spatialInfo *datatype.SpatialInfo) {
	if attrs == nil || item == nil || item.PointCloud == nil {
		return
	}
	metaattr.MergeAttributeMaps(attrs, metaattr.PointCloudInfoAttributes(item.PointCloud, spatialInfo))
}

func ApplyGaussianSplatInfo(attrs models.JSONMap, item *DetectedItem, spatialInfo *datatype.SpatialInfo) {
	if attrs == nil || item == nil || item.GaussianSplat == nil {
		return
	}
	metaattr.MergeAttributeMaps(attrs, metaattr.GaussianSplatInfoAttributes(item.GaussianSplat, spatialInfo))
}

func ApplyContainerInfo(attrs models.JSONMap, item *DetectedItem) {
	if attrs == nil || item == nil || item.Container == nil {
		return
	}
	metaattr.UpsertNested(attrs, "type_info", "container", metaattr.ContainerInfoAttributes(item.Container))
}

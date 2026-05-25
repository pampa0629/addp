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
	if attrs == nil || item == nil || item.DataType != dataitem.DataTypeContainer {
		return
	}
	metaattr.UpsertNested(attrs, "type_info", "container", metaattr.ContainerInfoAttributes(&datatype.ContainerInfo{
		Children:      []datatype.ContainerChildInfo{},
		ChildCount:    0,
		ResourceCount: 1,
	}))
}

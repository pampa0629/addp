package metaitem

import (
	"github.com/addp/common/dataitem"
	"github.com/addp/meta/internal/metaattr"
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

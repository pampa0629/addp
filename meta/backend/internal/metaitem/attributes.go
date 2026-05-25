package metaitem

import (
	"github.com/addp/meta/internal/metaattr"
)

func AttributeInput(item *DetectedItem) metaattr.DataItemAttributesInput {
	if item == nil {
		return metaattr.DataItemAttributesInput{}
	}
	return metaattr.DataItemAttributesInput{
		Attributes:   item.Attributes,
		Layout:       item.Layout,
		DataType:     item.DataType,
		Format:       item.Format,
		PhysicalPath: item.PhysicalPath,
		RefList:      item.RefList,
		SizeBytes:    item.SizeBytes,
	}
}

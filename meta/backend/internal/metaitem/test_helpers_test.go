package metaitem

import "github.com/addp/common/dataitem"

func detectedItemForTest(item dataitem.ResolvedItem) *DetectedItem {
	return &DetectedItem{ResolvedItem: item}
}

func int64PtrForTest(value int64) *int64 {
	return &value
}
